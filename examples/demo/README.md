# Demo control plane

A control plane you can provision against without a cloud account, and the
script for showing it off with [goose](https://block.github.io/goose/).

Everything here is composed from
[provider-nop](https://github.com/crossplane-contrib/provider-nop), whose
`NopResource` does nothing at all and reports whatever conditions it is told
to, after a delay. So a "database" created here is a fake database: it takes
twenty seconds to come up, publishes Ready and Synced like a real one, and
costs nothing. Swap the base resource in the Composition for an `RDSInstance`
and the same platform API, the same tools and the same conversation provision a
real one.

## What is in here

| File | What it defines |
| --- | --- |
| [00-packages.yaml](00-packages.yaml) | provider-nop and function-patch-and-transform |
| [10-xrd-database.yaml](10-xrd-database.yaml) | `PostgreSQLInstance`, a platform API for databases |
| [11-composition-database.yaml](11-composition-database.yaml) | What a database is made of |
| [20-xrd-workload.yaml](20-xrd-workload.yaml) | `App`, a platform API for workloads |
| [21-composition-workload.yaml](21-composition-workload.yaml) | What a workload is made of |

## Set it up

A cluster and Crossplane:

```bash
kind create cluster --name crossplane-demo

helm repo add crossplane-stable https://charts.crossplane.io/stable
helm repo update
helm install crossplane crossplane-stable/crossplane \
  --namespace crossplane-system --create-namespace --wait
```

The packages, then the platform APIs once the packages are healthy:

```bash
kubectl apply -f examples/demo/00-packages.yaml
kubectl wait provider.pkg/provider-nop --for=condition=Healthy --timeout=5m
kubectl wait function.pkg/function-patch-and-transform --for=condition=Healthy --timeout=5m

kubectl apply -f examples/demo/
kubectl wait xrd/xpostgresqlinstances.demo.crossplane.io --for=condition=Established
```

## Check the server can see it

```bash
crossplane-mcp-server call crossplane_xrds_list
```

You should see `xpostgresqlinstances.demo.crossplane.io` and
`xapps.demo.crossplane.io`.

Now ask for a database without creating one. `--read-only=false` is what makes
the write tools exist at all; without it the call fails with "no tool named":

```bash
crossplane-mcp-server --read-only=false call crossplane_database_create \
  '{"name":"orders-db","size":"small","storageGB":20,"dryRun":true}'
```

The answer is the exact manifest that would be submitted. Drop `dryRun` to
create it, then watch it come up:

```bash
crossplane-mcp-server call crossplane_resource_tree '{"kind":"PostgreSQLInstance","name":"orders-db"}'
```

## Demo it with goose

Start a session with the server attached, writes enabled:

```bash
goose session --with-extension "crossplane-mcp-server --read-only=false"
```

Or add it to `~/.config/goose/config.yaml` so every session has it:

```yaml
extensions:
  crossplane:
    enabled: true
    type: stdio
    cmd: crossplane-mcp-server
    args: ["--read-only=false"]
    timeout: 300
```

Then talk to it. The interesting part of this demo is that none of these asks
name a Kubernetes kind: goose finds the platform API, reads its schema and
fills it in.

```text
What platform APIs does this control plane offer?

Create a small Postgres database called orders-db. Show me the manifest before
you create anything.

Is it ready yet?

Why not?

Now create a workload called checkout-api running nginx.

Something is wrong with orders-db, have a look.

Give orders-db 50GB of storage instead.

Delete orders-db.
```

The last one is worth asking. The server has no delete tool, so goose will tell
you it cannot, and should offer `crossplane_impact` to show what the deletion
would take with it. You run the `kubectl delete` yourself.

## Clean up

```bash
kubectl delete postgresqlinstance --all
kubectl delete app --all
kind delete cluster --name crossplane-demo
```

Anything the server created carries the label
`app.kubernetes.io/created-by=crossplane-mcp-server`, so you can also find the
lot with
`kubectl get claim,composite -l app.kubernetes.io/created-by=crossplane-mcp-server -A`.
