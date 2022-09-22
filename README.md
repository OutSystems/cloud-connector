<h1 align="center">
<img src="images/neo-logo.png" />

OutSystems Cloud Connector
</h2>

![MIT][s0]

[s0]: https://img.shields.io/badge/license-MIT-blue.svg

## <a name="table-of-contents"></a> Table of Contents

1. [Overview](#overview)
1. [Install](#install)
    * [Binary](#binary)
    * [Docker](#docker)
    * [Firewall setup](#firewall-setup)
1. [Usage](#usage)
    * [Logging](#logging)
1. [Detailed options](#detailed-options)
1. [License](#license)

## 1. <a name="overview"></a> Overview <small><sup>[Top ▲](#table-of-contents)</sup></small>

The OutSystems Cloud Connector (`outsystemscc`) lets you connect the apps running in your OutSystems [Project Neo](https://www.outsystems.com/platform/project-neo/) organization to private data and private services  ("endpoints") that aren't accessible by the internet. `outsystemscc` is an open-source project written in Go.

You run `outsystemscc` on a system in your private network—an on-premises network, a private cloud, or the public cloud—to establish a secure tunnel between your endpoints and the Secure Gateway. Your apps can then access the endpoints through the Secure Gateway, the server component you activate for each stage of your Project Neo organization [using the Portal](https://www.outsystems.com/goto/secure-gateways). Common use cases include accessing data through a private REST API service and making requests to internal services (SMTP, SMB, NFS,..)

`outsystemscc` creates a fast TCP/UDP tunnel, with transport over HTTP via WebSockets, secured via SSH. The connection is established to either the built-in domain for the stage (for example `<customername>.outsystems.app`) or a custom domain configured for the stage (for example `example.com`). In both cases, the connection is over TLS and always encrypted with a valid X.509 certificate.

The following diagram shows an example Project Neo customer setup for a Secure Gateway active on two stages.

![Secure gateways diagram](images/secure-gateways-diag.png "Secure gateways diagram")

You see how to create a tunnel to the endpoints as in the example private customer on-premises data center in the [Usage](#usage) section.

## 2. <a name="install"></a> Install <small><sup>[Top ▲](#table-of-contents)</sup></small>

To install, use either the binary or Docker option. If your system supports it, there are several advantages of running `outsystemscc` as a Docker image as opposed to a binary.

* You always run the latest release. You don't need to reinstall each new release.
* It can run on Windows or any system that supports Docker.
* Without additional configuration it starts with the Docker daemon on system boot.
* For advanced use cases, you can use Kubernetes for orchestration.

> :information_source: After install, ensure you configure the firewall for the private network(s) correctly. Find guidance in the [Firewall setup](#firewall-setup) section.

### <a name="binary"></a> Binary

Download the latest release from the [releases page](https://github.com/OutSystems/cloud-connector/releases/latest). There are precompiled binaries available for Linux on i386 (32-bit), amd64 (64-bit), and arm64 (64-bit). You can run the binary on a Windows version that supports [WSL2](https://docs.microsoft.com/en-us/windows/wsl/).

To install, unzip/untar the package and then copy the binary to the desired location. For example:

    tar -zxvf outsystemscc_1.0.0_linux_amd64.tar.gz
    mv outsystemscc $HOME/.local/bin
    outsystemscc --help

You may want to configure the binary to run as a service so it can start on system boot, for example. See the documentation of your Linux distribution for detail on how to do this.

`outsystemscc` doesn't require root permissions to run.

### <a name="docker"></a> Docker

Run the Docker image directly from the OutSystems Docker Hub page:

    docker run --rm -it outsystems/outsystemscc --help

### <a name="firewall-setup"></a> Firewall setup

`outsystemscc` requires only outbound access to the internet in the private network(s) it's running.

You can restrict outbound internet connectivity (via a NAT Gateway, for example) by a firewall. For a Layer 7 firewall, you should allow outbound connections to the built-in domain (for example `<customername>.outsystems.app`) and any custom domains configured for the stage (for example `example.com`). For a Layer 4 firewall, you must open firewall rules to all [CloudFront IP ranges](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/LocationsOfEdgeServers.html) for port 443.

If the network requires outbound traffic to route through a proxy, you specify that using the `--proxy` option.

> :information_source: There may be a dedicated person or team at your organization responsible for administering network firewalls. If so, you may want to contact them for help with the process.


## 3. <a name="usage"></a> Usage <small><sup>[Top ▲](#table-of-contents)</sup></small>

The examples below use the binary command, `outsystemscc`. If you are using Docker, replace the command with `docker run --rm -it outsystems/outsystemscc`.

After using `outsystemscc` to connect one or more endpoints, you have a list of connected endpoint(s) of the form `secure-gateway:<port>`. You or a member of your team can then use these addresses directly in app development in Service Studio.

After you successfully activate the Secure Gateway for a stage in the Portal Project Neo, you see the following screen:

![Secure gateways in Portal](images/secure-gateways-pl.png "Secure gateways in Portal")

You use the **Token** and **Address** to form the `outsystemscc` command to run. For example:

    outsystemscc \
      --header "token: N2YwMDIxZTEtNGUzNS1jNzgzLTRkYjAtYjE2YzRkZGVmNjcy" \
      https://acme.outsystems.app/sg_6c23a5b4-b718-4634-a503-f22aed17d4e7 \
      R:8081:192.168.0.3:8393

In this example, you create a tunnel to the endpoint `192.168.0.3:8393`, a REST API service (HTTP/TCP) running on IP address `192.168.0.3`. The endpoint is available to consume by apps running in the connected stage at `secure-gateway:8081`.

You can create a tunnel to connect multiple endpoints to the same Secure Gateway. To do this, you can run multiple instances of `outsystemscc` or pass in multiple remotes (`R:<local-port>:<remote-host>:<remote-port>`) to the same instance. In the latter case, for example:

    outsystemscc \
      --header "token: N2YwMDIxZTEtNGUzNS1jNzgzLTRkYjAtYjE2YzRkZGVmNjcy" \
      https://acme.outsystems.app/sg_6c23a5b4-b718-4634-a503-f22aed17d4e7 \
      R:8081:192.168.0.3:8393 R:8082:192.168.0.4:587

In this example, you create a tunnel to connect two endpoints. One, as before, `192.168.0.3:8393`, a REST API service (HTTP/TCP) running on IP address `192.168.0.3`. The endpoint is available for use by apps running in the connected stage at `secure-gateway:8081`. Second, `192.168.0.4:587`, an SMTP server (SMTP/TCP) running on `192.168.0.4`, another IP in the internal address range. The endpoint is available for use by apps running in the connected stage at `secure-gateway:8082`.

You can create a tunnel to any endpoint that's in the internal address range and so network accessible over TCP or UDP from the system you run `outsystemscc` on. If the connection is over UDP, add `/udp` to the end of the remote port.

You or can learn more about using connected endpoints in app development on the [Project Neo documentation site](https://www.outsystems.com/goto/secure-gateways). Be sure to share the list of connected endpoint(s) of the form `secure-gateway:<port>` with members of your team responsible developing apps in Service Studio.

### <a name="logging"></a> Logging

By default, `outsystemscc` logs timestamped information to stdout. You can redirect this output to a file for retention purposes. For example:

    outsystemscc \
      --header "token: N2YwMDIxZTEtNGUzNS1jNzgzLTRkYjAtYjE2YzRkZGVmNjcy" \
      https://acme.outsystems.app/sg_6c23a5b4-b718-4634-a503-f22aed17d4e7 \
      R:8081:10.0.0.1:8393 \ 
      >> outsystemscc_log

If your organization uses a centralized log management product, see its documentation for how to redirect the log output into it.

## 4. <a name="detailed-options"></a> Detailed options <small><sup>[Top ▲](#table-of-contents)</sup></small>


You should keep remaining options as the default unless your network topology requires you to modify them.

```
  Usage: outsystemscc [options] <server> <remote> [remote] [remote] ...

  <server> is the URL to the server. Use the Address displayed on the Portal.

  <remote>s are remote connections tunneled through the server, each of
  which come in the form:

    R:<local-port>:<remote-host>:<remote-port>

  which does reverse port forwarding, sharing <remote-host>:<remote-port>
  from the client to the server's <local-port>.

    example remotes

	  R:8081:192.168.0.3:8393
    R:8082:192.168.0.4:587

    See https://github.com/OutSystems/cloud-connector for examples in context.
    
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
    Use the Token displayed on the Portal in using token as HeaderName.
	
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

## 5. <a name="license"></a> License <small><sup>[Top ▲](#table-of-contents)</sup></small>

[MIT](https://github.com/outsystems/cloud-connector/blob/master/LICENSE) © OutSystems
