# hello-eks

Demo of Dagger Go SDK by building a container and deploying it to an EKS cluster

Watch the demo at https://youtu.be/z6pYfz_q9tc

EKS deployed with [eksctl](https://eksctl.io/). It provides a single command to deploy all the required resources for a working EKS cluster.
`eksctl create cluster --name hello-eks --region us-east-1`

To run the pipeline, run: `go run ci/main.go`