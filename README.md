# kube-app-operator
Kubernetes Operator to support automatique deployment

# Define an App and Deploy it
This code is a sample to see how we can create a Customresource and intercat with it to talk to the classical kubernetes component.
In this sample, we watch pod event and customresource event.
On pod event, we just write a log
On customresource event, we get it , verify deployment and deploy(or delete) it.

# Version on client and server
Client Version: version.Info{Major:"1", Minor:"8", GitVersion:"v1.8.0", GitCommit:"6e937839ac04a38cac63e6a7a306c5d035fe7b0a", GitTreeState:"clean", BuildDate:"2017-09-28T22:57:57Z", GoVersion:"go1.8.3", Compiler:"gc", Platform:"linux/amd64"}
Server Version: version.Info{Major:"1", Minor:"7", GitVersion:"v1.7.5", GitCommit:"17d7182a7ccbb167074be7a87f0a68bd00d58d97", GitTreeState:"clean", BuildDate:"2017-10-06T20:53:14Z", GoVersion:"go1.8.3", Compiler:"gc", Platform:"linux/amd64"}



cd ~/cloudfabric/go/src/github.com/enablecloud/kube-app-operator
./kube-app-operator --kubeconfig=$HOME/.kube/config &
kubectl get customresourcedefinition
kubectl describe customresourcedefinition appfolders.cr.kube-app-operator.enablecloud.github.com
cat sample/simpleAppfolder.yaml
kubectl get deployments
kubectl get pods
kubectl create -f sample/simpleAppfolder.yaml
kubectl get deployments
kubectl get pods
kubectl delete appFolder test3
kubectl get deployments
kubectl get pods
