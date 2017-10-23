/*
Copyright 2014 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"

	"github.com/derekparker/delve/pkg/config"
	kubeappoperator "github.com/enablecloud/kube-app-operator/operator"
	"github.com/golang/example/stringutil"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	fmt.Println("Start")
	conf := &config.Config{}
	var eventHandler kubeappoperator.Handler

	kubeconfig := flag.String("kubeconfig", "", "Path to a kube config. Only required if out-of-cluster.")
	flag.Parse() // Create the client config. Use kubeconfig if given, otherwise assume in-cluster.
	config, err := buildConfig(*kubeconfig)
	if err != nil {
		panic(err)
	}
	eventHandler = new(kubeappoperator.Default)
	eventHandler.Init(conf, kubeappoperator.GetClientOutOfCluster())
	apiextensionsclientset, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	// initialize custom resource using a CustomResourceDefinition if it does not exist
	crd, err := kubeappoperator.CreateCustomResourceDefinition(apiextensionsclientset)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		panic(err)
	}

	if crd != nil {
		defer apiextensionsclientset.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(crd.Name, nil)
	}

	kubeappoperator.Start(conf, config, eventHandler)
	fmt.Println(stringutil.Reverse("!selpmaxe oG ,olleH"))

}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}
