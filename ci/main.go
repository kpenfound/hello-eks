package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"dagger.io/dagger"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/eks"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

const (
	publishAddress = "kylepenfound/hello-eks:latest"
	eksCluster     = "hello-eks"
	eksNamespace   = "hello-eks"
	eksDeployment  = "hello-eks"
	eksService     = "hello-eks"
	awsRegion      = "us-east-1"
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
	_ = client.Container().
		From("alpine").
		WithMountedFile("/tmp/hello", build).
		Exec(dagger.ContainerExecOpts{
			Args: []string{"cp", "/tmp/hello", "/bin/hello"},
		}).
		WithEntrypoint([]string{"/bin/hello"}) //.
		//Publish(ctx, publishAddress)
	// if err != nil {
	// 	panic(err)
	// }

	addr := "docker.io/kylepenfound/hello-eks:latest@sha256:1ab30ec999e3e68edc03dc08c27794d177438b919fa03d797f64f67ab9c0164b"
	fmt.Println(addr)
	err = deploy(ctx, addr)
	if err != nil {
		fmt.Printf("Error deploying hello-eks: %v", err)
	}
	fmt.Printf("Updated %s deployment\n", eksDeployment)
}

func deploy(ctx context.Context, imageref string) error {
	// get kube client
	clientset, err := getKubeClient(ctx)
	if err != nil {
		return err
	}
	// get pod or service?
	return rollingDeployment(ctx, clientset, imageref)
}

func rollingDeployment(ctx context.Context, clientset *kubernetes.Clientset, imageref string) error {
	deployments := clientset.AppsV1().Deployments("default")

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := deployments.Get(ctx, eksDeployment, metav1.GetOptions{})
		if err != nil {
			return err
		}

		result.Spec.Template.Spec.Containers[0].Image = imageref
		_, err = deployments.Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
}

// With help from https://stackoverflow.com/questions/60547409/unable-to-obtain-kubeconfig-of-an-aws-eks-cluster-in-go-code/60573982#60573982
func getKubeClient(ctx context.Context) (*kubernetes.Clientset, error) {
	// Get EKS service
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(awsRegion),
	}))
	eksSvc := eks.New(sess)

	// Get cluster
	input := &eks.DescribeClusterInput{
		Name: aws.String(eksCluster),
	}
	cluster, err := eksSvc.DescribeCluster(input)
	if err != nil {
		return nil, fmt.Errorf("Error calling DescribeCluster: %v", err)
	}
	// Get token
	gen, err := token.NewGenerator(true, false)
	if err != nil {
		return nil, err
	}
	opts := &token.GetTokenOptions{
		ClusterID: aws.StringValue(cluster.Cluster.Name),
	}
	tok, err := gen.GetWithOptions(opts)
	if err != nil {
		return nil, err
	}
	// b64 decode CA
	ca, err := base64.StdEncoding.DecodeString(aws.StringValue(cluster.Cluster.CertificateAuthority.Data))
	if err != nil {
		return nil, err
	}
	// create k8s clientset
	return kubernetes.NewForConfig(
		&rest.Config{
			Host:        aws.StringValue(cluster.Cluster.Endpoint),
			BearerToken: tok.Token,
			TLSClientConfig: rest.TLSClientConfig{
				CAData: ca,
			},
		},
	)
}
