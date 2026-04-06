package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"math/rand"

	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"

	chclient "github.com/jpillora/chisel/client"
	"github.com/jpillora/chisel/share/cos"
	"github.com/jpillora/chisel/share/settings"
)

var (
	version = "dev" // Set by goreleaser
)

func main() {
	client(os.Args[1:])
}

func generatePidFile() {
	pid := []byte(strconv.Itoa(os.Getpid()))
	if err := os.WriteFile("outsystemscc.pid", pid, 0644); err != nil {
		log.Fatal(err)
	}
}

type headerFlags struct {
	http.Header
}

func (flag *headerFlags) String() string {
	out := ""
	for k, v := range flag.Header {
		out += fmt.Sprintf("%s: %s\n", k, v)
	}
	return out
}

func (flag *headerFlags) Set(arg string) error {
	index := strings.Index(arg, ":")
	if index < 0 {
		return fmt.Errorf(`invalid header (%s). Should be in the format "HeaderName: HeaderContent"`, arg)
	}
	if flag.Header == nil {
		flag.Header = http.Header{}
	}
	key := arg[0:index]
	value := arg[index+1:]
	flag.Header.Set(key, strings.TrimSpace(value))
	return nil
}

type jsonEvent struct {
	CorrelationID  string      `json:"correlation_id"`
	Time           int64       `json:"time"`
	Host           string      `json:"host"`
	Source         string      `json:"source"`
	Sourcetype     string      `json:"source_type"`
	Event          tunnelEvent `json:"event"`
}

type tunnelEvent struct {
	Version        string   `json:"version"`
	Server         string   `json:"server"`
	Remotes   	   []string `json:"remotes"`
	Status         string   `json:"status"`
	Latency        *string  `json:"latency"`    // null when not yet known
	Error          *string  `json:"error"`      // null on success
}

func emitObsEvent(correlationID, status, server string, remotes []string,
	latency *string, obsErr *string) {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	ev := jsonEvent{
		CorrelationID: 	 correlationID,
		Time:            time.Now().UnixMilli(),
		Host:            hostname,
		Source:          "outsystemscc",
		Sourcetype:      "outsystemscc:tunnel",
		Event: tunnelEvent{
			Version:   version,
			Server:    server,
			Remotes:   remotes,
			Status:    status,
			Latency:   latency,
			Error:     obsErr,
		},
	}
	data, err := json.Marshal(ev)
	if err != nil {
		log.Printf("[WARN] observability: failed to marshal event: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

var clientHelp = `
  Usage: outsystemscc [options] <server> <remote> [remote] [remote] ...

  <server> is the URL to the server. Use the Address displayed on ODC Portal.

  <remote>s are remote connections tunneled through the server, each of
  which come in the form:

    R:<local-port>:<remote-host>:<remote-port>

  which does reverse port forwarding, sharing <remote-host>:<remote-port>
  from the client to the server's <local-port>.

    example remotes

      R:8081:192.168.0.3:8393
	  R:8082:192.168.0.4:587

    See https://github.com/OutSystems/cloud-connector for  examples in context.
    
  Options:

    --keepalive, An optional keepalive interval. Since the underlying
    transport is HTTP, in many instances we'll be traversing through
    proxies, often these proxies will close idle connections. You must
    specify a time with a unit, for example '5s' or '2m'. Defaults
    to '25s' (set to 0s to disable).

    --max-retry-count, Maximum number of times to retry before exiting.
    Defaults to unlimited.

    --max-retry-interval, Maximum wait time before retrying after a
    disconnection. Defaults to 5 minutes.

    --proxy, An optional HTTP CONNECT or SOCKS5 proxy which will be
    used to reach the server. Authentication can be specified
    inside the URL.
    For example, http://admin:password@my-server.com:8081
            or: socks://admin:password@my-server.com:1080

    --header, Set a custom header in the form "HeaderName: HeaderContent". 
	Use the Token displayed on ODC Portal in using token as HeaderName.

	--pid Generate pid file in current working directory

    -o, Emit JSON events to stdout at key tunnel lifecycle points (starting,
	connected, disconnected, error). Each event is a single-line JSON object
	including destination hosts, connection status, and latency.

    -v, Enable verbose logging

    --help, This help text

  Signals:
    The outsystemscc process is listening for:
      a SIGUSR2 to print process stats, and
      a SIGHUP to short-circuit the client reconnect timer

  Version:
    ` + version + ` (` + runtime.Version() + `)
`

func client(args []string) {
	flags := flag.NewFlagSet("client", flag.ContinueOnError)
	config := chclient.Config{Headers: http.Header{}}
	flags.DurationVar(&config.KeepAlive, "keepalive", 25*time.Second, "")
	flags.IntVar(&config.MaxRetryCount, "max-retry-count", -1, "")
	flags.DurationVar(&config.MaxRetryInterval, "max-retry-interval", 0, "")
	flags.StringVar(&config.Proxy, "proxy", "", "")
	flags.Var(&headerFlags{config.Headers}, "header", "")
	hostname := flags.String("hostname", "", "Deprecated, will be ignored")
	pid := flags.Bool("pid", false, "")
	verbose := flags.Bool("v", false, "")
	observability := flags.Bool("o", false, "")
	flags.Usage = func() {
		fmt.Print(clientHelp)
		os.Exit(0)
	}
	flags.Parse(args)

	// Set custom User-Agent
	if config.Headers.Get("User-Agent") == "" {
		config.Headers.Set("User-Agent", fmt.Sprintf("CloudConnector/%s", version))
	}

	//pull out options, put back remaining args
	args = flags.Args()
	if len(args) < 2 {
		log.Fatalf("A server and least one remote is required")
	}

	localPorts, err := validateRemotes(args[1:])
	if err != nil {
		log.Fatal(err)
	}

	queryParams := generateQueryParameters(localPorts)

	//get server URL
	serverURL := fetchURL(createHTTPClient(&config), args[0])

	config.Server = fmt.Sprintf("%s%s", serverURL, queryParams)
	config.Remotes = args[1:]

	var correlationID string
	if *observability {
		correlationID = uuid.New().String()
		emitObsEvent(correlationID, "starting", serverURL, args[1:], nil, nil)
	}

	//default auth
	if config.Auth == "" {
		config.Auth = os.Getenv("AUTH")
	}
	//move hostname onto headers
	if *hostname != "" {
		log.Printf("[WARN] The --hostname flag will be removed in a future release. Please consider removing it to avoid breaking changes.\n" +
			"Please specify the correct server URL directly instead.\n")
	}
	//ready
	c, err := chclient.NewClient(&config)
	if err != nil {
		log.Fatal(err)
	}
	c.Debug = *verbose
	if *pid {
		generatePidFile()
	}
	go cos.GoStats()
	ctx := cos.InterruptContext()
	connectStart := time.Now()
	if err := c.Start(ctx); err != nil {
		if *observability {
			errStr := err.Error()
			emitObsEvent(correlationID, "error", serverURL, args[1:], nil, &errStr)
		}
		log.Fatal(err)
	}
	if *observability {
		ms := time.Since(connectStart).String()
		emitObsEvent(correlationID, "connected", serverURL, args[1:], &ms, nil)
	}
	if err := c.Wait(); err != nil {
		if *observability {
			errStr := err.Error()
			emitObsEvent(correlationID, "error", serverURL, args[1:], nil, &errStr)
		}
		log.Fatal(err)
	}
	if *observability {
		emitObsEvent(correlationID, "disconnected", serverURL, args[1:], nil, nil)
	}
}

func createHTTPClient(config *chclient.Config) *resty.Client {
	client := resty.New()

	// Set proxy if configured
	if config.Proxy != "" {
		client.SetProxy(config.Proxy)
	}

	return client
}

func fetchURL(client *resty.Client, requestLocation string) string {

	client.SetRedirectPolicy(resty.NoRedirectPolicy())

	if !strings.HasPrefix(requestLocation, "http") {
		requestLocation = "http://" + requestLocation
	}

	resp, err := client.SetDoNotParseResponse(true).R().Get(requestLocation)
	if err != nil {
		if resp != nil && resp.StatusCode() == http.StatusFound {
			redirectURL := resp.Header().Get("Location")
			if redirectURL == "" {
				log.Fatalf("Redirect response missing 'Location' header")
			}
			return redirectURL
		} else {
			log.Fatalf("Failed to fetch URL '%s': %v", requestLocation, err)
		}
	}

	return requestLocation
}

func generateQueryParameters(localPorts string) string {
	return fmt.Sprintf("?id=%v&ports=%v", rand.Intn(999999999-100000000)+100000000, localPorts)
}

// validate the provided Remotes configuration is valid
func validateRemotes(remotes []string) (string, error) {
	uniqueRemotes := []string{}
	localPorts := []string{}

	for _, newRemote := range remotes {

		remote, err := settings.DecodeRemote(newRemote)
		if err != nil {
			return "", fmt.Errorf("failed to decode remote '%s': %s", newRemote, err)
		}

		// iterate all remotes already in the unique list, if duplicate is found return error
		for _, unique := range uniqueRemotes {
			validatedRemote, err := settings.DecodeRemote(unique)
			if err != nil {
				return "", fmt.Errorf("failed to decode remote '%s': %s", unique, err)
			}

			if isDuplicatedRemote(validatedRemote, remote) {
				return "", fmt.Errorf("invalid Remote configuration: local port '%s' is duplicated", remote.LocalPort)
			}
		}

		uniqueRemotes = append(uniqueRemotes, newRemote)
		localPorts = append(localPorts, remote.LocalPort)
	}

	return strings.Join(localPorts, ","), nil
}

func isDuplicatedRemote(first, second *settings.Remote) bool {
	return first.LocalPort == second.LocalPort
}
