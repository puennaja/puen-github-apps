package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/puennaja/puen-github-apps/internal/implementer"
)

func main() {
	authFile := flag.String("authorization-file", "", "operator-owned assignment JSON")
	auditFile := flag.String("audit-file", "", "private audit JSONL")
	network := flag.Bool("allow-network", false, "operator-approved activation only")
	flag.Parse()
	if *authFile == "" || *auditFile == "" {
		fmt.Fprintln(os.Stderr, "broker_startup_failed")
		os.Exit(1)
	}
	env := map[string]string{}
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	config, err := implementer.ConfigFromEnv(env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "broker_startup_failed")
		os.Exit(1)
	}
	key, err := implementer.EnvironmentKey(config.PrivateKeyRef)
	if err != nil {
		fmt.Fprintln(os.Stderr, "broker_startup_failed")
		os.Exit(1)
	}
	// This CLI intentionally has no secret-manager plugin loader. A trusted embedding
	// can resolve secret:// references via an approved external source.
	raw, err := os.ReadFile(*authFile)
	if err != nil || len(raw) > 8192 {
		fmt.Fprintln(os.Stderr, "broker_startup_failed")
		os.Exit(1)
	}
	var grant implementer.Authorization
	if json.Unmarshal(raw, &grant) != nil {
		fmt.Fprintln(os.Stderr, "broker_startup_failed")
		os.Exit(1)
	}
	audit, err := implementer.OpenJSONLAudit(*auditFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "broker_startup_failed")
		os.Exit(1)
	}
	defer audit.Close()
	broker, err := implementer.NewFrameworkBroker(config, key, *network)
	if err != nil {
		fmt.Fprintln(os.Stderr, "broker_startup_failed")
		os.Exit(1)
	}
	gateway, err := implementer.NewGateway(config, grant, broker, audit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "broker_startup_failed")
		os.Exit(1)
	}
	if implementer.Serve(context.Background(), gateway, os.Stdin, os.Stdout) != nil {
		fmt.Fprintln(os.Stderr, "broker_stream_failed")
		os.Exit(1)
	}
}
