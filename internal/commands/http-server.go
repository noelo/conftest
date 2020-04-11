package commands

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/instrumenta/conftest/parser"
	"github.com/instrumenta/conftest/policy"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewHTTPCommand creates http server
func NewHTTPCommand(ctx context.Context) *cobra.Command {
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
			h2 := func(w http.ResponseWriter, _ *http.Request) {
				io.WriteString(w, "Hello from a HandleFunc #2!\n")
			}

			http.HandleFunc("/", h1)
			http.HandleFunc("/endpoint", h2)
			var sPort = strconv.Itoa(portNum)

			log.Fatal(http.ListenAndServe(":"+sPort, nil))

			return nil
		},
	}
	cmd.Flags().IntVar(&portNum, "port", 8080, "port to listen on")
	return &cmd
}

func doWork(ctx context.Context) error {
	out := GetOutputManager(outputJSON, false)
	input := viper.GetString("input")

	files, err := parseFileList(fileList)
	if err != nil {
		return fmt.Errorf("parse files: %w", err)
	}

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
