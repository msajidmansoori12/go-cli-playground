# go-cli-playground

A small workspace for Go CLI experiments. The main project here is **mini-crane**, a Kubernetes learning CLI that exports cluster resources to YAML files on disk (similar in spirit to tools like [Crane](https://github.com/konveyor/crane) for migration workflows, but minimal and focused on export).

## Prerequisites

- **Go** — see `mini-crane/go.mod` for the toolchain version (currently `go 1.25.6`).
- A **Kubernetes cluster** and a working **kubeconfig** (same conventions as `kubectl`).

## mini-crane

### Build

```bash
cd mini-crane
go build -o mini-crane .
```

### Usage

```bash
./mini-crane export [flags]
```

The command connects using standard Kubernetes client flags (from `kubectl`-style options, e.g. `--kubeconfig`, `--context`, `--namespace`).

### What it does

- Writes manifests under **`export/resources/<namespace>/`** (relative to the current working directory).
- Filenames are derived from kind, API group, version, namespace, and resource name, for example:
  - `Deployment_apps_v1_default_my-app.yaml`

### Flags

| Flag | Description |
|------|-------------|
| `--kind` | Export only one resource type (e.g. `pods`, `deployments`, `services`; short names supported). |
| `--selector` | Kubernetes label selector (e.g. `app=nginx`). |
| `--all-namespaces` | List resources across all namespaces (namespace in kubeconfig is ignored for listing). |

### Export behavior

- **Without `--kind`**: Iterates server-preferred API resources, exports **namespaced** types that support **list**, skips subresources (names containing `/`), and skips a small set of kinds (e.g. `Event`, auth review types). Errors during list for a given type are skipped so the run can continue.
- **With `--kind`**: Resolves the resource and exports only that type.

If some API groups fail discovery, a warning is logged and export continues.

### Stack

- [Cobra](https://github.com/spf13/cobra) for the CLI
- [client-go](https://github.com/kubernetes/client-go) (dynamic client, discovery, REST mapper)
- [logrus](https://github.com/sirupsen/logrus) for logs
- [sigs.k8s.io/yaml](https://github.com/kubernetes-sigs/yaml) for YAML output

## Repository layout

```
go-cli-playground/
├── README.md
└── mini-crane/
    ├── main.go
    ├── cmd/export/     # export subcommand
    ├── go.mod
    └── export/         # created when you run export (example output may be present)
```

## License

This repository does not include a license file by default; add one if you intend to share or reuse the code.
