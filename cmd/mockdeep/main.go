// mockdeep is a tiny HTTP server used to exercise agent-deepweb e2e.
// Each auth style has its own endpoint accepting one hardcoded credential.
// Run `mockdeep -help` for the endpoint map.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/shhac/agent-deepweb/internal/mockdeep"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "listen address")
	showCreds := flag.Bool("creds", false, "print the hardcoded valid credentials and exit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "mockdeep — demo server for agent-deepweb e2e tests")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Routes:")
		for _, line := range mockdeep.Routes() {
			fmt.Fprintln(os.Stderr, "  "+line)
		}
		fmt.Fprintln(os.Stderr, "")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showCreds {
		fmt.Printf("Bearer token:     %s\n", mockdeep.ValidBearerToken)
		fmt.Printf("API key header:   %s: %s\n", mockdeep.APIKeyHeader, mockdeep.ValidAPIKey)
		fmt.Printf("Basic auth:       %s:%s\n", mockdeep.ValidUsername, mockdeep.ValidPassword)
		fmt.Printf("Login issues:     Bearer %s  +  Cookie session=%s\n", mockdeep.LoginToken, mockdeep.SessionCookie)
		return
	}

	srv := mockdeep.New()
	log.Printf("mockdeep listening on http://%s (hit / for route map)", *addr)
	if err := http.ListenAndServe(*addr, srv); err != nil {
		log.Fatal(err)
	}
}
