module github.com/outsystems/cloud-connector

require github.com/jpillora/chisel v1.9.1

require (
	github.com/armon/go-socks5 v0.0.0-20160902184237-e75332964ef5 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/jpillora/sizestr v1.0.0 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/net v0.15.0 // indirect
	golang.org/x/sync v0.3.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
)

replace (
	github.com/fsnotify/fsnotify => github.com/fsnotify/fsnotify v1.7.0
	golang.org/x/crypto => golang.org/x/crypto v0.14.0
	golang.org/x/net => golang.org/x/net v0.17.0
	golang.org/x/sync => golang.org/x/sync v0.4.0
	golang.org/x/sys => golang.org/x/sys v0.13.0
)

replace github.com/jpillora/chisel => github.com/outsystems/chisel v1.9.2-0.20230908114230-bf0e1ad1c3e6

go 1.21
