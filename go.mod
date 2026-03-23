module github.com/outsystems/cloud-connector

require (
	github.com/go-resty/resty/v2 v2.17.2
	github.com/jarcoal/httpmock v1.4.1
	github.com/jpillora/chisel v1.10.1
)

require (
	github.com/armon/go-socks5 v0.0.0-20160902184237-e75332964ef5 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/jpillora/sizestr v1.0.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/jpillora/chisel => github.com/outsystems/chisel v1.11.3-os.4

go 1.25.8
