package main

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/config"
)

// newConfigCmd is the group `config` (ADR 0020).
func newConfigCmd(app *cli.App) *cobra.Command {
	return cli.Group("config", "Inspect the configuration", newConfigCheckCmd(app))
}

// configCheckJSON documents what `config check --output json` prints.
const configCheckJSON = "one object: databaseUrl, httpAddr, baseUrl (strings), demoMode (bool), smtp {host, " +
	"port (number, 0: the default of the TLS mode), tls (empty: starttls), username, password, from}, " +
	"trustedProxies (array of strings), hstsIncludeSubdomains (bool), metricsAddr, log {format, level}, " +
	"push {publicKey, privateKey, subject}, as; an empty string is an unset setting, and the secrets " +
	"(the database password, smtp.password, push.privateKey) are xxxxx when set"

func newConfigCheckCmd(app *cli.App) *cobra.Command {
	var (
		flags serveFlags
		cmd   *cobra.Command
	)

	return app.Command(cli.Spec{
		Use:   "check",
		Short: "Validate the configuration and print it with the secrets redacted",
		Long: "Load the configuration as serve does, from the environment and serve's flags, without " +
			"starting anything: no database, no mail server. An invalid value exits with status 2 and " +
			"names the variable or flag. Otherwise it prints every effective setting by its environment " +
			"variable, with the secrets redacted (the database password, " + config.EnvSMTPPassword + ", " +
			config.EnvVAPIDPrivateKey + "). " + config.EnvDemoMode + " is the effective mode: true only " +
			"without a database. With --output json it prints " + configCheckJSON + ".",
		Example: "  studio-schedule config check\n" +
			"  studio-schedule config check --base-url https://schedule.example.org --output json",
		JSON:  configCheckJSON,
		Flags: func(c *cobra.Command) { cmd = c; flags.register(c) },
		Run: func(_ context.Context, env *cli.Env, _ []string) error {
			cfg, err := env.Config(flags.explicit(cmd))
			if err != nil {
				return err
			}

			view := newConfigView(cfg)

			return env.Out.Print(view, func(t *cli.Text) {
				t.Table("Setting", "Value")

				for _, row := range view.rows() {
					t.Row(row[0], row[1])
				}
			})
		},
	})
}

// configView is the effective configuration with the secrets redacted, the
// JSON of `config check`.
type configView struct {
	DatabaseURL           string   `json:"databaseUrl"`
	HTTPAddr              string   `json:"httpAddr"`
	BaseURL               string   `json:"baseUrl"`
	DemoMode              bool     `json:"demoMode"`
	SMTP                  smtpView `json:"smtp"`
	TrustedProxies        []string `json:"trustedProxies"`
	HSTSIncludeSubdomains bool     `json:"hstsIncludeSubdomains"`
	MetricsAddr           string   `json:"metricsAddr"`
	Log                   logView  `json:"log"`
	Push                  pushView `json:"push"`
	As                    string   `json:"as"`
}

type smtpView struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLS      string `json:"tls"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
}

type logView struct {
	Format string `json:"format"`
	Level  string `json:"level"`
}

type pushView struct {
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
	Subject    string `json:"subject"`
}

// newConfigView redacts cfg (Config.Redacted).
func newConfigView(cfg config.Config) configView {
	r := cfg.Redacted()

	proxies := make([]string, len(r.TrustedProxies))
	for i, p := range r.TrustedProxies {
		proxies[i] = p.String()
	}

	format := config.DefaultLogFormat
	if r.Log.JSON {
		format = "json"
	}

	return configView{
		DatabaseURL: r.DatabaseURL,
		HTTPAddr:    r.HTTPAddr,
		BaseURL:     r.BaseURL,
		// The effective mode, as Config.LogValue reports it: DEMO_MODE does
		// nothing with a database.
		DemoMode: r.Demo && r.DatabaseURL == "",
		SMTP: smtpView{
			Host: r.SMTP.Host, Port: r.SMTP.Port, TLS: r.SMTP.TLS, Username: r.SMTP.Username,
			Password: r.SMTP.Password, From: r.SMTP.From,
		},
		TrustedProxies:        proxies,
		HSTSIncludeSubdomains: r.HSTSIncludeSubdomains,
		MetricsAddr:           r.MetricsAddr,
		Log:                   logView{Format: format, Level: strings.ToLower(r.Log.Level.String())},
		Push:                  pushView{PublicKey: r.Push.PublicKey, PrivateKey: r.Push.PrivateKey, Subject: r.Push.Subject},
		As:                    r.As,
	}
}

// rows are the settings by their environment variables, for the text
// output; an unset setting is empty.
func (v configView) rows() [][2]string {
	port := ""
	if v.SMTP.Port != 0 {
		port = strconv.Itoa(v.SMTP.Port)
	}

	return [][2]string{
		{config.EnvDatabaseURL, v.DatabaseURL},
		{config.EnvHTTPAddr, v.HTTPAddr},
		{config.EnvBaseURL, v.BaseURL},
		{config.EnvDemoMode, strconv.FormatBool(v.DemoMode)},
		{config.EnvSMTPHost, v.SMTP.Host},
		{config.EnvSMTPPort, port},
		{config.EnvSMTPTLS, v.SMTP.TLS},
		{config.EnvSMTPUsername, v.SMTP.Username},
		{config.EnvSMTPPassword, v.SMTP.Password},
		{config.EnvSMTPFrom, v.SMTP.From},
		{config.EnvTrustedProxies, strings.Join(v.TrustedProxies, ",")},
		{config.EnvHSTSIncludeSubdomains, strconv.FormatBool(v.HSTSIncludeSubdomains)},
		{config.EnvMetricsAddr, v.MetricsAddr},
		{config.EnvLogFormat, v.Log.Format},
		{config.EnvLogLevel, v.Log.Level},
		{config.EnvVAPIDPublicKey, v.Push.PublicKey},
		{config.EnvVAPIDPrivateKey, v.Push.PrivateKey},
		{config.EnvVAPIDSubject, v.Push.Subject},
		{config.EnvAs, v.As},
	}
}
