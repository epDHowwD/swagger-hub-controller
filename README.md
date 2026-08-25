# Swagger UI kubernetes controller

[![release](https://img.shields.io/github/release/DoodleScheduling/swagger-hub-controller/all.svg)](https://github.com/DoodleScheduling/swagger-hub-controller/releases)
[![release](https://github.com/doodlescheduling/swagger-hub-controller/actions/workflows/release.yaml/badge.svg)](https://github.com/doodlescheduling/swagger-hub-controller/actions/workflows/release.yaml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/DoodleScheduling/swagger-hub-controller/badge)](https://api.securityscorecards.dev/projects/github.com/DoodleScheduling/swagger-hub-controller)
[![Coverage Status](https://coveralls.io/repos/github/DoodleScheduling/swagger-hub-controller/badge.svg?branch=master)](https://coveralls.io/github/DoodleScheduling/swagger-hub-controller?branch=master)
[![license](https://img.shields.io/github/license/DoodleScheduling/swagger-hub-controller.svg)](https://github.com/DoodleScheduling/swagger-hub-controller/blob/master/LICENSE)

This controller manages deployments of [Swagger UI](https://swagger.io/tools/swagger-ui/).
The controller can lookup `SwaggerDefinition` references and hook them up with a `SwaggerHub`.
Each `SwaggerHub` is a managed [Swagger UI](https://swagger.io/tools/swagger-ui/) deployment which includes the related swagger definitions.

This approach is great for microservices which have their own OpenAPI specs but a unified swagger-ui is wanted.

### Beta API notice
For v0.x releases and beta api we try not to break the API specs. However
in rare cases backports happen to fix major issues.

## Example

```yaml
apiVersion: swagger.infra.doodle.com/v1beta1
kind: SwaggerHub
metadata:
  name: default
spec:
  definitionSelector:
    matchLabels: {}
```

If no definition selector on the hub is configured no definitions will be included.
`matchLabels: {}` will include all of them in the same namespace as the hub.
By using match labels or expressions it can be configured what definitions should be included in the hub.

Similar to the `definitionSelector` it is possible to match definitions cross namespace by using `spec.namespaceSelector`. 
By default a `SwaggerHub` only looks up definitions from the same namespace as the hub but with a namespace selector this behaviour can be changed.
Using `namespaceSelector.matchLabels: {}` will lookup definitions across all namespaces.

```yaml
apiVersion: swagger.infra.doodle.com/v1beta1
kind: SwaggerDefinition
metadata:
  name: microservice-a
  namespace: default
  labels:
    scope: external-api
spec:
  url: https://microservice-a/openapispecs/v1
---
apiVersion: swagger.infra.doodle.com/v1beta1
kind: SwaggerDefinition
metadata:
  name: microservice-b
  namespace: default
  labels:
    scope: external-api
spec:
  url: https://microservice-b/swagger-specs
```

## Authenticated definitions

A `SwaggerDefinition` which is served behind https with basic auth can reference a secret holding the credentials.
The credentials are only used by `SwaggerSpecification` which fetches definitions server side, the secret must
be in the same namespace as the `SwaggerDefinition`.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: microservice-a-credentials
  namespace: default
stringData:
  username: swagger
  password: secret
---
apiVersion: swagger.infra.doodle.com/v1beta1
kind: SwaggerDefinition
metadata:
  name: microservice-a
  namespace: default
spec:
  url: https://microservice-a/openapispecs/v1
  auth:
    basic:
      secretRef:
        name: microservice-a-credentials
        # optional, defaults shown
        usernameField: username
        passwordField: password
```

If the username is a fixed value it can be configured inline instead, in which case only the password
is looked up in the secret and `secretRef.usernameField` is ignored:

```yaml
  auth:
    basic:
      username: swagger
      secretRef:
        name: microservice-a-credentials
```

**Note**: By default credentials are only sent over `https`. A definition which configures `spec.auth` with a plain
`http://` url is rejected and reported as an error on the specification instead of leaking the credentials.
For cluster internal services which are not served over TLS this can be opted out of explicitly:

```yaml
  url: http://microservice-a/openapispecs/v1
  auth:
    basic:
      allowInsecure: true
      secretRef:
        name: microservice-a-credentials
```

## Deployment template
It is possible to define a custom swagger-ui deployment template which the controller will use to spin up the managed deployment.
In the following example the deployment receives an additional container called mysidecar. Also resources
are declared for the `swagger-ui` container.

**Note**: The swagger-ui container is always called swagger-ui. It is possible to patch that container by using said name as in the example bellow.

```yaml
apiVersion: swagger.infra.doodle.com/v1beta1
kind: SwaggerHub
metadata:
  name: default
spec:
  definitionSelector:
    matchLabels: {}
  deploymentTemplate:
    spec:
      template:
        replicas: 3
        spec:
          containers:
          - name: swagger-ui
            resources:
              requests:
                memory: 256Mi
                cpu: 50m
              limits:
                memory: 512Mi
          - name: random-sidecar
            image: mysidecar
```

## Unify specifications

By utilizing `SwaggerSpecification` resources it is possible to actually merge multiple `SwaggerDefinitions` into one single swagger specification.
This is useful to represent a collection of `SwaggerDefinitions` as a single entity.

The `SwaggerSpecification` needs to know which `SwaggerDefinitions` is should look up by setting a `spec.definitionSelector` just as with the `SwaggerHub`.
By default the first server occurrence from the first SwaggerDefinition is used in the new merged specification. However this might not be wanted therefore the OpenAPI servers
as well as the info specifications can be overwritten just as in the example bellow:

```yaml
apiVersion: swagger.infra.doodle.com/v1beta1
kind: SwaggerSpecification
metadata:
  name: my-api
  namespace: default
spec:
  info: 
    title: My API
  servers:
  - url: https://api.gateway.net
    description: example api entrypoint
  definitionSelector:
    matchLabels:
      scope: external-api #Selects all SwaggerDefinitions from above
---
apiVersion: swagger.infra.doodle.com/v1beta1
kind: SwaggerHub
metadata:
  name: default
spec:
  specificationSelector:
    matchLabels: {}
  definitionSelector:
    matchLabels:
      scope: other-definitions
  deploymentTemplate:
    spec:
      template:
        replicas: 3
        spec:
          containers:
          - name: swagger-ui
            resources:
              requests:
                memory: 256Mi
                cpu: 50m
              limits:
                memory: 512Mi
          - name: random-sidecar
            image: mysidecar
```


## Suspend/Resume reconciliation

The reconciliation can be paused by setting `spec.suspend` to `true`:
 
```
kubectl patch swaggerhub default-p '{"spec":{"suspend": true}}' --type=merge
```

## Observe SwaggerHub reconciliation

A `SwaggerHub` will have all discovered resources populated in `.status.subResourceCatalog`.
Also there are two conditions which are useful for observing `Ready` and a temporary one named `Reconciling`
as long as a reconciliation is in progress.

## Installation

### Helm

Please see [chart/swagger-hub-controller](https://github.com/DoodleScheduling/swagger-hub-controller/tree/master/chart/swagger-hub-controller) for the helm chart docs.

### Manifests/kustomize

Alternatively you may get the bundled manifests in each release to deploy it using kustomize or use them directly.

## Configuration
The controller can be configured using cmd args:
```
--concurrent int                            The number of concurrent SwaggerHub reconciles. (default 4)
--enable-leader-election                    Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.
--graceful-shutdown-timeout duration        The duration given to the reconciler to finish before forcibly stopping. (default 10m0s)
--health-addr string                        The address the health endpoint binds to. (default ":9557")
--insecure-kubeconfig-exec                  Allow use of the user.exec section in kubeconfigs provided for remote apply.
--insecure-kubeconfig-tls                   Allow that kubeconfigs provided for remote apply can disable TLS verification.
--kube-api-burst int                        The maximum burst queries-per-second of requests sent to the Kubernetes API. (default 300)
--kube-api-qps float32                      The maximum queries-per-second of requests sent to the Kubernetes API. (default 50)
--leader-election-lease-duration duration   Interval at which non-leader candidates will wait to force acquire leadership (duration string). (default 35s)
--leader-election-release-on-cancel         Defines if the leader should step down voluntarily on controller manager shutdown. (default true)
--leader-election-renew-deadline duration   Duration that the leading controller manager will retry refreshing leadership before giving up (duration string). (default 30s)
--leader-election-retry-period duration     Duration the LeaderElector clients should wait between tries of actions (duration string). (default 5s)
--log-encoding string                       Log encoding format. Can be 'json' or 'console'. (default "json")
--log-level string                          Log verbosity level. Can be one of 'trace', 'debug', 'info', 'error'. (default "info")
--max-retry-delay duration                  The maximum amount of time for which an object being reconciled will have to wait before a retry. (default 15m0s)
--metrics-addr string                       The address the metric endpoint binds to. (default ":9556")
--min-retry-delay duration                  The minimum amount of time for which an object being reconciled will have to wait before a retry. (default 750ms)
--watch-all-namespaces                      Watch for resources in all namespaces, if set to false it will only watch the runtime namespace. (default true)
--watch-label-selector string               Watch for resources with matching labels e.g. 'sharding.fluxcd.io/shard=shard1'.
```
