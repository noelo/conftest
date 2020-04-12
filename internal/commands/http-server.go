package commands

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"

	"github.com/instrumenta/conftest/parser"
	"github.com/instrumenta/conftest/policy"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewHTTPCommand creates http server
func NewHTTPCommand(ctx context.Context) *cobra.Command {
	log.SetFormatter(&log.JSONFormatter{})
	var portNum int
	cmd := cobra.Command{
		Use:   "http-server <port>",
		Short: "Provide a http endpoint to test configuration files",
		Long:  "Provide a http endpoint which receives and tests your configuration files using Open Policy Agent",

		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("%v\n", portNum)

			h1 := func(w http.ResponseWriter, _ *http.Request) {
				io.WriteString(w, "Hello from a HandleFunc #1!\n")
			}

			http.HandleFunc("/", h1)
			http.HandleFunc("/validate", handleHTTPPostRequest)
			var sPort = strconv.Itoa(portNum)

			log.Fatal(http.ListenAndServe(":"+sPort, nil))

			return nil
		},
	}
	cmd.Flags().IntVar(&portNum, "port", 8080, "port to listen on")
	return &cmd
}

func handleHTTPPostRequest(w http.ResponseWriter, r *http.Request) {
	log.Info("Handling PostRequest", r)
	parseErr := r.ParseMultipartForm(32 << 20) // maxMemory 32MB
	if parseErr != nil {
		log.Error("Failed to parse multipart message")
		http.Error(w, "failed to parse multipart message", http.StatusBadRequest)
		return
	}

	numFiles := len(r.MultipartForm.File)
	valFiles := make([]string, numFiles)
	fileCount := 0
	for key, value := range r.MultipartForm.File {
		f, _, _ := r.FormFile(key)
		metadata, _ := ioutil.ReadAll(f)
		log.WithFields(log.Fields{
			"k":    key,
			"v":    value,
			"size": len(metadata),
		}).Info("File read ")
		valFiles[fileCount] = string(metadata)
		fileCount++
	}

	doWork(nil, valFiles)

	io.WriteString(w, "Hello from a handleHTTPPostRequest #1!\n")
}

func doWork(ctx context.Context, files []string) error {
	out := GetOutputManager(outputJSON, false)
	input := viper.GetString("input")
	// files := []string{"null1", "null2"}

	// files, err := parseFileList(fileList)
	// if err != nil {
	// 	return fmt.Errorf("parse files: %w", err)
	// }

	configurations, err := parser.GetConfigurations(ctx, input, files)
	if err != nil {
		return fmt.Errorf("get configurations: %w", err)
	}

	policyPath := viper.GetString("policy")

	regoFiles, err := policy.ReadFiles(policyPath)
	if err != nil {
		return fmt.Errorf("read rego files: %w", err)
	}

	compiler, err := policy.BuildCompiler(regoFiles)
	if err != nil {
		return fmt.Errorf("build compiler: %w", err)
	}

	dataPaths := viper.GetStringSlice("data")
	store, err := policy.StoreFromDataFiles(dataPaths)
	if err != nil {
		return fmt.Errorf("build store: %w", err)
	}

	testRun := TestRun{
		Compiler: compiler,
		Store:    store,
	}

	var namespaces []string
	namespaces, err = policy.GetNamespaces(regoFiles, compiler)

	var failureFound bool
	result, err := testRun.GetResult(ctx, namespaces, configurations)
	if err != nil {
		return fmt.Errorf("get combined test result: %w", err)
	}

	if isResultFailure(result) {
		failureFound = true
	}

	result.FileName = "Combined"
	if err := out.Put(result); err != nil {
		return fmt.Errorf("writing combined error: %w", err)
	}

	if err := out.Flush(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}

	if failureFound {
		os.Exit(1)
	}

	return nil

}
