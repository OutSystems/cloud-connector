# OutSystems Cloud Connector

The OutSystems Cloud Connector (`outsystemscc`) allows applications running in the OutSystems cloud to securely access remote services running in a private network through the OutSystems Secure Gateway. It is based in the open source component [chisel](https://github.com/jpillora/chisel), which is a fast TCP/UDP tunnel, transported over HTTP, secured via SSH. 

With `outsystemscc` you establish a secure tunnel from your private network (e.g. on-prem or private cloud) to the applications running in OutSystems cloud, while keeping full control and auditability of what it is exposed to your applications.

## Install

### Binaries
Download the latest release from the [releases page](https://github.com/OutSystems/cloud-connector/releases/latest). 
Unzip/untar and copy the executable to the desired location, for example:
```sh
tar -xzf outsystemscc_1.0.0_linux_amd64.tar.gz
mv outsystemscc /usr/local/bin
./outsystemscc --help
```

### Docker

```sh
docker run --rm -it outsystems/outsystemscc --help
```

## Usage
```plain
 Usage: outsystemscc [options] <server> <remote> [remote] [remote] ...

  <server> is the URL to the server.

  <remote>s are remote connections tunneled through the server, each of
  which come in the form:

    R:<local-port>:<remote-host>:<remote-port>

  which does reverse port forwarding, sharing <remote-host>:<remote-port>
  from the client to the server's <local-port>.

    example remotes

      R:2222:localhost:22
      R:8080:10.0.0.1:80
    
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
    Can be used multiple times. (e.g --header "Foo: Bar" --header "Hello: World")

    --hostname, Optionally set the 'Host' header (defaults to the host
    found in the server url).

	--pid Generate pid file in current working directory

    -v, Enable verbose logging

    --help, This help text

  Signals:
    The outsystemscc process is listening for:
      a SIGUSR2 to print process stats, and
      a SIGHUP to short-circuit the client reconnect timer
```

## License

[MIT](https://github.com/outsystems/cloud-connector/blob/master/LICENSE) © OutSystems