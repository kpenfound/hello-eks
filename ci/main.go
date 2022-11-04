package main

import (
	"context"
	"fmt"
	"os"

	"dagger.io/dagger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	publishAddress = "registry.digitalocean.com/hello-doks/hello:latest"
	doksCluster    = "mycluster"
	doksNamespace  = "mynamespace"
	doksDeployment = "mypod-deployment"
	doksService    = "myservice"
	doksPod        = "mypod"
)

func main() {
	// Create dagger client
	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stdout))
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Build our app
	project := client.Host().Workdir()

	builder := client.Container().
		From("golang:latest").
		WithMountedDirectory("/src", project).
		WithWorkdir("/src").
		WithEnvVariable("GOOS", "linux").
		WithEnvVariable("GOARCH", "amd64").
		WithEnvVariable("CGO_ENABLED", "0").
		Exec(dagger.ContainerExecOpts{
			Args: []string{"go", "build", "-o", "hello"},
		})

	// Get built binary
	build := builder.File("/src/hello")

	// Publish binary on Alpine base
	addr, err := client.Container().
		From("alpine").
		WithMountedFile("/tmp/hello", build).
		Exec(dagger.ContainerExecOpts{
			Args: []string{"cp", "/tmp/hello", "/bin/hello"},
		}).
		WithEntrypoint([]string{"/bin/hello"}).
		Publish(ctx, publishAddress)
	if err != nil {
		panic(err)
	}

	fmt.Println(addr)

	_ = deploy(ctx, addr)
}

func deploy(ctx context.Context, imageref string) error {
	// get kube client
	clientset, err := getKubeClient(ctx)
	if err != nil {
		return err
	}
	// get pod or service?
	return createDeployment(ctx, clientset, imageref)
}

func getKubeClient(ctx context.Context) (dynamic.Interface, error) {
	// Configure kube client
	kubeconfig := []byte{}
	clientconfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, err
	}
	apiconfig, err := clientconfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(apiconfig)
}

// Based on https://github.com/kubernetes/client-go/blob/d576a3570dbe44f39c31a3ad341450f29aefeb8d/examples/dynamic-create-update-delete-deployment/main.go
func createDeployment(ctx context.Context, client dynamic.Interface, imageref string) error {
	deploymentRes := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	deployment := getDeploymentDefinition(imageref)

	result, err := client.Resource(deploymentRes).Namespace(doksNamespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created deployment %q.\n", result.GetName())
	return nil
}

// is this _really_ the way?
func getDeploymentDefinition(imageref string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name": doksDeployment,
			},
			"spec": map[string]interface{}{
				"replicas": 2,
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": doksService,
					},
				},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"app": doksService,
						},
					},

					"spec": map[string]interface{}{
						"containers": []map[string]interface{}{
							{
								"name":  doksPod,
								"image": imageref,
								"ports": []map[string]interface{}{
									{
										"name":          "http",
										"protocol":      "TCP",
										"containerPort": 8080,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
