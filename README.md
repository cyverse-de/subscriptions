# subscriptions

An HTTP microservice implementing a subset of the functionality in github.com/cyverse/QMS. This will eventually
become the replacement for QMS.

## Local Development Testing

### Prerequisites

#### PostgreSQL

It's necessary to have access to the QMS database, either on the local host or on a server somewhere. You can find
instructions for setting up the QMS database in the [QMS README file][1].

#### Configuration

The configuration file has to be available. The source configuration file is in the `k8s-resources` repository under
`$K8S_RESOURCES_DIR/resources/configs/$ENVIRONMENT_NAME/jobservices.yml`, and the default path expected by the
`subscriptions` service is `/etc/cyverse/de/configs/service.yml`. You can make the configuration file available either
by putting a copy of the configuration file at that path or by specifying the path to the configuration file using the
`--config` command-line option.

A dotenv file can also be used to specify configuration settings. This file can be used to set environment variables
in the format `QMS_SOME_CONFIGURATION_SETTING`, for example `QMS_DATABASE_URI`. The default path for the dotenv file
is `/etc/cyverse/de/env/service.env`. You can also specify a different path using the `--dotenv-path` command-line
option.

The final way that you can specify configuration settings is by setting environment variables in your shell before
starting the service.

The easiest way to specify the configuration is by using a `dotenv` file in the local directory, for example:

```
QMS_USERNAME_SUFFIX=@iplantcollaborative.org
QMS_DATABASE_URI=postgresql://de@localhost/qms?sslmode=disable
```

### Optional but Useful

#### jq

You can use `jq` to format the responses from the `subscriptions` service to make them more readable. See the
[jq website][2] for more information.

### Starting the Service

Once you've built the `subscriptions` service and the QMS database is available, you can start the service with:

```
$ make
$ ./subscriptions --dotenv-path dotenv
```

The service listens on port 60000 by default; use `--port` to change that.

### Sending Requests

The service is a plain HTTP API, so any HTTP client works. The routes are registered in `app/app.go`. Here's an
example that retrieves a user's subscription summary:

```
$ curl -s http://localhost:60000/summary/sarahr | jq
```

[1]: https://github.com/cyverse/QMS
[2]: https://jqlang.github.io/jq/
