/*
Operator

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
package kubeappoperator

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/derekparker/delve/pkg/config"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

const maxRetries = 5

type Handler interface {
	Init(c *config.Config) error
	ObjectCreated(obj interface{})
	ObjectDeleted(obj interface{})
	ObjectUpdated(oldObj, newObj interface{})
}

// Controller object
type Controller struct {
	clientset    kubernetes.Interface
	queue        workqueue.RateLimitingInterface
	informer     cache.SharedIndexInformer
	eventHandler Handler
}

// GetClient returns a k8s clientset to the request from inside of cluster
func GetClient() kubernetes.Interface {
	config, err := rest.InClusterConfig()
	if err != nil {
		fmt.Println("Can not get kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Println("Can not create kubernetes client: %v", err)
	}

	return clientset
}

func buildOutOfClusterConfig() (*rest.Config, error) {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = os.Getenv("HOME") + "/.kube/config"
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}

// GetClientOutOfCluster returns a k8s clientset to the request from outside of cluster
func GetClientOutOfCluster() kubernetes.Interface {
	config, err := buildOutOfClusterConfig()
	if err != nil {
		fmt.Println("Can not get kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)

	return clientset
}

func Start(conf *config.Config, eventHandler Handler) {
	kubeClient := GetClientOutOfCluster()

	c := newControllerPod(kubeClient, eventHandler)
	stopCh := make(chan struct{})
	defer close(stopCh)

	go c.Run(stopCh)

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGTERM)
	signal.Notify(sigterm, syscall.SIGINT)
	<-sigterm

	//if conf.Resource.Services {
	//	watchServices(kubeClient, eventHandler)
	//}
	//
	//if conf.Resource.ReplicationController {
	//	watchReplicationControllers(kubeClient, eventHandler)
	//}
	//
	//if conf.Resource.Deployment {
	//	watchDeployments(kubeExtensionsClient, eventHandler)
	//}
	//
	//if conf.Resource.Job {
	//	watchJobs(kubeExtensionsClient, eventHandler)
	//}
	//
	//if conf.Resource.PersistentVolume {
	//	var servicesStore cache.Store
	//	servicesStore = watchPersistenVolumes(kubeClient, servicesStore, eventHandler)
	//}

	//logrus.Fatal(http.ListenAndServe(":8081", nil))
}

func newControllerPod(client kubernetes.Interface, eventHandler Handler) *Controller {
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options meta_v1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().Pods(meta_v1.NamespaceAll).List(options)
			},
			WatchFunc: func(options meta_v1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().Pods(meta_v1.NamespaceAll).Watch(options)
			},
		},
		&v1.Pod{},
		0, //Skip resync
		cache.Indexers{},
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
	})

	return &Controller{
		clientset:    client,
		informer:     informer,
		queue:        queue,
		eventHandler: eventHandler,
	}
}

// Run starts the kubewatch controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	fmt.Println("Starting kubewatch controller")

	go c.informer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	fmt.Println("Kubewatch controller synced and ready")

	wait.Until(c.runWorker, time.Second, stopCh)
}

// HasSynced is required for the cache.Controller interface.
func (c *Controller) HasSynced() bool {
	return c.informer.HasSynced()
}

// LastSyncResourceVersion is required for the cache.Controller interface.
func (c *Controller) LastSyncResourceVersion() string {
	return c.informer.LastSyncResourceVersion()
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
		// continue looping
	}
}

func (c *Controller) processNextItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.processItem(key.(string))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(key)
	} else if c.queue.NumRequeues(key) < maxRetries {
		fmt.Println("Error processing %s (will retry): %v", key, err)
		c.queue.AddRateLimited(key)
	} else {
		// err != nil and too many retries
		fmt.Println("Error processing %s (giving up): %v", key, err)
		c.queue.Forget(key)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *Controller) processItem(key string) error {
	fmt.Println("Processing change to Pod %s", key)

	obj, exists, err := c.informer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("Error fetching object with key %s from store: %v", key, err)
	}

	if !exists {
		c.eventHandler.ObjectDeleted(obj)
		return nil
	}

	c.eventHandler.ObjectCreated(obj)
	return nil
}

//
//func watchServices(client *client.Client, eventHandler handlers.Handler) cache.Store {
//	//Define what we want to look for (Services)
//	watchlist := cache.NewListWatchFromClient(client, "services", api.NamespaceAll, fields.Everything())
//
//	resyncPeriod := 30 * time.Minute
//
//	//Setup an informer to call functions when the watchlist changes
//	eStore, eController := framework.NewInformer(
//		watchlist,
//		&api.Service{},
//		resyncPeriod,
//		framework.ResourceEventHandlerFuncs{
//			AddFunc:    eventHandler.ObjectCreated,
//			DeleteFunc: eventHandler.ObjectDeleted,
//			UpdateFunc: eventHandler.ObjectUpdated,
//		},
//	)
//
//	//Run the controller as a goroutine
//	go eController.Run(wait.NeverStop)
//
//	return eStore
//}
//
//func watchReplicationControllers(client *client.Client, eventHandler handlers.Handler) cache.Store {
//	//Define what we want to look for (ReplicationControllers)
//	watchlist := cache.NewListWatchFromClient(client, "replicationcontrollers", api.NamespaceAll, fields.Everything())
//
//	resyncPeriod := 30 * time.Minute
//
//	//Setup an informer to call functions when the watchlist changes
//	eStore, eController := framework.NewInformer(
//		watchlist,
//		&api.ReplicationController{},
//		resyncPeriod,
//		framework.ResourceEventHandlerFuncs{
//			AddFunc:    eventHandler.ObjectCreated,
//			DeleteFunc: eventHandler.ObjectDeleted,
//		},
//	)
//
//	//Run the controller as a goroutine
//	go eController.Run(wait.NeverStop)
//
//	return eStore
//}
//
//func watchDeployments(client *client.ExtensionsClient, eventHandler handlers.Handler) cache.Store {
//	//Define what we want to look for (Deployments)
//	watchlist := cache.NewListWatchFromClient(client, "deployments", api.NamespaceAll, fields.Everything())
//
//	resyncPeriod := 30 * time.Minute
//
//	//Setup an informer to call functions when the watchlist changes
//	eStore, eController := framework.NewInformer(
//		watchlist,
//		&v1beta1.Deployment{},
//		resyncPeriod,
//		framework.ResourceEventHandlerFuncs{
//			AddFunc:    eventHandler.ObjectCreated,
//			DeleteFunc: eventHandler.ObjectDeleted,
//		},
//	)
//
//	//Run the controller as a goroutine
//	go eController.Run(wait.NeverStop)
//
//	return eStore
//}
//
//func watchJobs(client *client.ExtensionsClient, eventHandler handlers.Handler) cache.Store {
//	//Define what we want to look for (Jobs)
//	watchlist := cache.NewListWatchFromClient(client, "jobs", api.NamespaceAll, fields.Everything())
//
//	resyncPeriod := 30 * time.Minute
//
//	//Setup an informer to call functions when the watchlist changes
//	eStore, eController := framework.NewInformer(
//		watchlist,
//		&v1beta1.Job{},
//		resyncPeriod,
//		framework.ResourceEventHandlerFuncs{
//			AddFunc:    eventHandler.ObjectCreated,
//			DeleteFunc: eventHandler.ObjectDeleted,
//		},
//	)
//
//	//Run the controller as a goroutine
//	go eController.Run(wait.NeverStop)
//
//	return eStore
//}
//
//func watchPersistenVolumes(client *client.Client, store cache.Store, eventHandler handlers.Handler) cache.Store {
//	//Define what we want to look for (PersistenVolumes)
//	watchlist := cache.NewListWatchFromClient(client, "persistentvolumes", api.NamespaceAll, fields.Everything())
//
//	resyncPeriod := 30 * time.Minute
//
//	//Setup an informer to call functions when the watchlist changes
//	eStore, eController := framework.NewInformer(
//		watchlist,
//		&api.PersistentVolume{},
//		resyncPeriod,
//		framework.ResourceEventHandlerFuncs{
//			AddFunc:    eventHandler.ObjectCreated,
//			DeleteFunc: eventHandler.ObjectDeleted,
//		},
//	)
//
//	//Run the controller as a goroutine
//	go eController.Run(wait.NeverStop)
//
//	return eStore
//}

// Default handler implements Handler interface,
// print each event with JSON format
type Default struct {
}

// Init initializes handler configuration
// Do nothing for default handler
func (d *Default) Init(c *config.Config) error {
	return nil
}

func (d *Default) ObjectCreated(obj interface{}) {

}

func (d *Default) ObjectDeleted(obj interface{}) {

}

func (d *Default) ObjectUpdated(oldObj, newObj interface{}) {

}
