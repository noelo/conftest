package commands

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"

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
			http.HandleFunc("/validate", handleHTTPPostRequest)
			var sPort = strconv.Itoa(portNum)
			fmt.Println("Starting HTTP Server on port", sPort)
			log.Fatal(http.ListenAndServe(":"+sPort, nil))
			return nil
		},
	}
	cmd.Flags().IntVar(&portNum, "port", 8080, "port to listen on")
	return &cmd
}

func handleHTTPPostRequest(w http.ResponseWriter, r *http.Request) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	log.Debug("Handling PostRequest", r)
	parseErr := r.ParseMultipartForm(32 << 20) // maxMemory 32MB
	if parseErr != nil {
		log.Error("Failed to parse multipart message")
		http.Error(w, "failed to parse multipart message", http.StatusBadRequest)
		return
	}

	fileData := make(map[string]multipart.File)
	for key, value := range r.MultipartForm.File {
		fl, _, _ := r.FormFile(key)
		fileData[value[0].Filename] = fl
	}

	resStr, err := doWork(ctx, fileData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	fmt.Fprintf(w, "%s", resStr)
}

func doWork(ctx context.Context, filesContent map[string]multipart.File) (string, error) {
	out := NewJSONOutputManager(nil)

	configurations, err := GetConfigurationsHTTP(ctx, filesContent)
	if err != nil {
		return "", fmt.Errorf("get configurations: %w", err)
	}

	policyPath := viper.GetString("policy")

	regoFiles, err := policy.ReadFiles(policyPath)
	if err != nil {
		return "", fmt.Errorf("read rego files: %w", err)
	}

	compiler, err := policy.BuildCompiler(regoFiles)
	if err != nil {
		return "", fmt.Errorf("build compiler: %w", err)
	}

	dataPaths := viper.GetStringSlice("data")
	store, err := policy.StoreFromDataFiles(dataPaths)
	if err != nil {
		return "", fmt.Errorf("build store: %w", err)
	}

	testRun := TestRun{
		Compiler: compiler,
		Store:    store,
	}

	var namespaces []string

	namespaces, err = policy.GetNamespaces(regoFiles, compiler)
	if err != nil {
		return "", fmt.Errorf("get namespaces: %w", err)
	}

	for fileName, config := range configurations {
		result, err := testRun.GetResult(ctx, namespaces, config)
		if err != nil {
			return "", fmt.Errorf("get test result: %w", err)
		}

		result.FileName = fileName
		if err := out.Put(result); err != nil {
			return "", fmt.Errorf("writing error: %w", err)
		}
	}

	var resultStr string
	if resultStr, err = out.FlushToString(); err != nil {
		return "", fmt.Errorf("flushing output: %w", err)
	}

	return resultStr, nil
}

// GetConfigurations parses and returns the configurations given in the file list
func GetConfigurationsHTTP(ctx context.Context, filesContent map[string]multipart.File) (map[string]interface{}, error) {
	var fileConfigs []parser.ConfigDoc
	for fileName, fileMP := range filesContent {
		fileType := parser.GetFileType(fileName, "")
		fileparser, err := parser.GetParser(fileType)
		if err != nil {
			return nil, fmt.Errorf("get parser: %w", err)
		}

		configDoc := parser.ConfigDoc{
			ReadCloser: fileMP,
			Filepath:   fileName,
			Parser:     fileparser,
		}

		fileConfigs = append(fileConfigs, configDoc)
	}

	unmarshaledConfigs, err := parser.BulkUnmarshal(fileConfigs)
	if err != nil {
		return nil, fmt.Errorf("bulk unmarshal: %w", err)
	}

	return unmarshaledConfigs, nil
}
