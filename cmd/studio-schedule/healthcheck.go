package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/config"
)

// healthcheckTimeout bounds the whole request, below the image's
// HEALTHCHECK --timeout.
const healthcheckTimeout = 3 * time.Second

// healthPath is the liveness endpoint that the health check requests.
const healthPath = "/healthz"

func newHealthcheckCmd(env config.LookupFunc) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Check that the local server answers (for container health checks)",
		Long: "Request " + healthPath + " from the server on this host, at the port of the listen address " +
			"(" + config.EnvHTTPAddr + " or --addr, as for serve), and exit with status 0 if it answers 200 " +
			"OK, 1 otherwise. The container image's HEALTHCHECK runs it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var flags config.Flags
			if cmd.Flags().Changed(config.FlagHTTPAddr) {
				flags.HTTPAddr = &addr
			}

			cfg, err := config.LoadWithoutLog(env, flags)
			if err != nil {
				return err
			}

			return healthcheck(cmd.Context(), healthURL(cfg.HTTPAddr))
		},
	}

	cmd.Flags().StringVar(&addr, config.FlagHTTPAddr, config.DefaultHTTPAddr,
		"listen address host:port of the server (env "+config.EnvHTTPAddr+")")

	return cmd
}

// healthURL is the health endpoint of a server listening on the validated
// address addr: an unspecified host (":8080", "0.0.0.0:8080", "[::]:8080")
// is reached on 127.0.0.1, a specific one directly.
func healthURL(addr string) string {
	host, port, _ := net.SplitHostPort(addr) // validated by config.Load

	if ip, err := netip.ParseAddr(host); host == "" || err == nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}

	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: healthPath}).String()
}

// healthcheck requests target and fails unless it answers 200 OK in time.
func healthcheck(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}

	// No proxy from the environment, no redirects: only this server counts.
	client := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check: %s answered %s", target, resp.Status)
	}

	return nil
}
