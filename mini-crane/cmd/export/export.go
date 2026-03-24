package export

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/yaml"
)

type ExportOptions struct {
	configFlags   *genericclioptions.ConfigFlags
	exportDir     string
	namespace     string
	kind          string
	selector      string
	allNamespaces bool
	genericclioptions.IOStreams
}

func NewExportCommand(streams genericclioptions.IOStreams) *cobra.Command {
	o := &ExportOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		IOStreams:   streams,
	}

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export resources from the cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd, args); err != nil {
				return err
			}
			if o.allNamespaces {
				fmt.Fprintln(o.Out, "Namespace: all namespaces")
			} else {
				fmt.Fprintln(o.Out, "Namespace: ", o.namespace)
			}

			if err := o.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&o.kind, "kind", "", "Export only a specified resource (pods , deployments , services) ")
	cmd.Flags().StringVar(&o.selector, "selector", "", "Label selector (example: app=nginx)")
	cmd.Flags().BoolVar(&o.allNamespaces, "all-namespaces", false, "Export resources across all namespaces")
	o.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func (o *ExportOptions) Complete(cmd *cobra.Command, args []string) error {
	ns, _, err := o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	o.namespace = ns
	return nil
}
func (o *ExportOptions) effectiveNamespace() string {
	if o.allNamespaces {
		return ""
	}
	return o.namespace
}
func (o *ExportOptions) Run() error {
	log := newLogger()
	resourceDir, err := prepareExportDir()
	if err != nil {
		return err
	}

	restConfig, dynamicClient, discoveryClient, mapper, err := createClients(o.configFlags)
	if err != nil {
		return err
	}
	resourcesList, err := discoverResources(discoveryClient)
	if discovery.IsGroupDiscoveryFailedError(err) {
		log.Warn("some api groups failed discovery")
	} else if err != nil {
		return err
	}
	if err != nil {
		return err
	}
	log.Infof("Connected to: %s", restConfig.Host)
	if o.selector != "" {
		log.Infof("Using label selector: %s", o.selector)
	}

	err = o.exportResources(dynamicClient, resourcesList, resourceDir, mapper, log)
	if err != nil {
		return err
	}

	log.Info("Export completed successfully")
	return nil
}

func hasVerb(resource v1.APIResource, verb string) bool {
	for _, v := range resource.Verbs {
		if v == verb {
			return true
		}
	}
	return false
}

func newLogger() *logrus.Logger {
	log := logrus.New()
	log.SetLevel(logrus.InfoLevel)
	return log
}

func prepareExportDir() (string, error) {
	exportDir := "export"
	resourceDir := filepath.Join(exportDir, "resources")
	err := os.MkdirAll(resourceDir, 0700)
	if err != nil {
		return "", err
	}
	return resourceDir, nil
}

func createClients(configFlags *genericclioptions.ConfigFlags) (*rest.Config, dynamic.Interface, *discovery.DiscoveryClient, *restmapper.DeferredDiscoveryRESTMapper, error) {
	restConfig, err := configFlags.ToRESTConfig()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(
		memory.NewMemCacheClient(discoveryClient),
	)
	return restConfig, dynamicClient, discoveryClient, mapper, nil
}

func resolveKind(mapper meta.RESTMapper, resourcesList []*v1.APIResourceList, input string) (schema.GroupVersionResource, error) {

	input = strings.ToLower(input)

	for _, group := range resourcesList {
		for _, resource := range group.APIResources {
			// match full resource name
			if resource.Name == input {
				return mapper.ResourceFor(schema.GroupVersionResource{
					Resource: resource.Name,
				})
			}

			for _, short := range resource.ShortNames {
				if short == input {
					return mapper.ResourceFor(schema.GroupVersionResource{
						Resource: resource.Name,
					})
				}
			}
		}
	}
	return schema.GroupVersionResource{}, fmt.Errorf("resource %q not found", input)
}

func discoverResources(discoveryClient *discovery.DiscoveryClient) ([]*v1.APIResourceList, error) {
	return discoveryClient.ServerPreferredResources()
}

func (o *ExportOptions) exportResources(dynamicClient dynamic.Interface, resourcesList []*v1.APIResourceList, resourceDir string, mapper *restmapper.DeferredDiscoveryRESTMapper, log *logrus.Logger) error {
	if o.kind != "" {
		return o.exportSingleResource(dynamicClient, mapper, resourcesList, resourceDir, log)
	}
	for _, group := range resourcesList {
		for _, resource := range group.APIResources {
			if strings.Contains(resource.Name, "/") {
				continue
			}
			verbListMatch := hasVerb(resource, "list")
			if !verbListMatch || !resource.Namespaced {
				continue
			}

			if skipResource(resource) {
				continue
			}

			gv, err := schema.ParseGroupVersion(group.GroupVersion)

			gvr := schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: resource.Name,
			}

			obj, err := dynamicClient.Resource(gvr).Namespace(o.effectiveNamespace()).List(
				context.Background(),
				v1.ListOptions{
					LabelSelector: o.selector,
				},
			)

			if err != nil {
				continue
			}

			if len(obj.Items) > 0 {
				ns := o.effectiveNamespace()
				if ns == "" {
					ns = "multiple"
				}
				log.Infof("Exporting %s/%s (%d objects)", resource.Name, ns, len(obj.Items))
				groupPart := gv.Group
				if groupPart == "" {
					groupPart = "core"
				}
				for _, item := range obj.Items {
					ns := item.GetNamespace()
					if ns == "" {
						ns = "cluster"
					}
					nsDir := filepath.Join(resourceDir, ns)
					os.MkdirAll(nsDir, 0700)
					filename := fmt.Sprintf(
						"%s_%s_%s_%s_%s.yaml",
						item.GetKind(),
						groupPart,
						gv.Version,
						item.GetNamespace(),
						item.GetName(),
					)
					log.Debugf("Writing file %s", filename)
					path := filepath.Join(nsDir, filename)
					data, err := yaml.Marshal(item.Object)
					if err != nil {
						log.Warnf("Skipping resources %s: %v", resource.Name, err)
						continue
					}
					err = os.WriteFile(path, data, 0644)
					if err != nil {
						log.Warnf("Failed writing %s: %v", filename, err)
						continue
					}

				}
			}
		}
	}
	return nil
}

func skipResource(resource v1.APIResource) bool {
	skipKinds := map[string]bool{
		"Event":                   true,
		"Binding":                 true,
		"TokenReview":             true,
		"SubjectAccessReview":     true,
		"SelfSubjectAccessReview": true,
		"SelfSubjectRulesReview":  true,
	}
	if skipKinds[resource.Kind] {
		return true
	}
	return false
}

func (o *ExportOptions) exportSingleResource(dynamicClient dynamic.Interface,
	mapper *restmapper.DeferredDiscoveryRESTMapper,
	resourcesList []*v1.APIResourceList, resourceDir string, log *logrus.Logger) error {
	targetGVR, err := resolveKind(mapper, resourcesList, o.kind)
	if err != nil {
		return fmt.Errorf("Unknown resource type: %s", o.kind)
	}
	obj, err := dynamicClient.Resource(targetGVR).Namespace(o.effectiveNamespace()).List(
		context.Background(),
		v1.ListOptions{
			LabelSelector: o.selector,
		},
	)
	if err != nil {
		return err
	}
	if len(obj.Items) == 0 {
		log.Infof("No %s resources found", targetGVR.Resource)
		return nil
	}
	ns := o.effectiveNamespace()
	if ns == "" {
		ns = "multiple"
	}
	log.Infof("Exporting %s/%s (%d objects)", targetGVR.Resource, ns, len(obj.Items))

	for _, item := range obj.Items {
		ns := item.GetNamespace()
		if ns == "" {
			ns = "cluster"
		}
		nsDir := filepath.Join(resourceDir, ns)
		os.MkdirAll(nsDir, 0700)
		gvk := item.GroupVersionKind()

		group := gvk.Group
		if group == "" {
			group = "core"
		}
		filename := fmt.Sprintf(
			"%s_%s_%s_%s_%s.yaml",
			item.GetKind(),
			group,
			gvk.Version,
			item.GetNamespace(),
			item.GetName(),
		)
		path := filepath.Join(nsDir, filename)
		data, err := yaml.Marshal(item.Object)
		if err != nil {
			log.Warnf("Skipping resource %s: %v", targetGVR.Resource, err)
			continue
		}
		err = os.WriteFile(path, data, 0644)
		if err != nil {
			log.Warnf("Failed writing %s: %v", filename, err)
			continue
		}
	}
	return nil
}
