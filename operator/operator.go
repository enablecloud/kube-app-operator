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
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/derekparker/delve/pkg/config"
	crv1 "github.com/enablecloud/kube-app-operator/apis/cr/v1"
	"k8s.io/api/core/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/api"
)

const maxRetries = 5

type Handler interface {
	Init(c *config.Config, kube kubernetes.Interface) error
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

func Start(conf *config.Config, cfg *rest.Config, eventHandler Handler) {
	kubeClient := GetClientOutOfCluster()

	c := newControllerPod(kubeClient, eventHandler)
	stopCh := make(chan struct{})
	defer close(stopCh)

	go c.Run(stopCh)

	// make a new config for our extension's API group, using the first config as a baseline
	appClient, appScheme, err := NewClient(cfg)
	if err != nil {
		panic(err)
	}

	// start a controller on instances of our custom resource
	controller := AppFolderController{
		AppFolderClient: appClient,
		AppFolderScheme: appScheme,
	}
	fmt.Println(controller.AppFolderScheme)
	appF := watchAppFolder(kubeClient, appClient, eventHandler)
	go appF.Run(stopCh)
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

func NewClient(cfg *rest.Config) (*rest.RESTClient, *runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	if err := crv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	config := *cfg
	config.GroupVersion = &crv1.SchemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, nil, err
	}

	return client, scheme, nil
}

type AppFolderController struct {
	AppFolderClient *rest.RESTClient
	AppFolderScheme *runtime.Scheme
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

func watchAppFolder(clientkub kubernetes.Interface, client *rest.RESTClient, eventHandler Handler) *Controller {

	//Define what we want to look for (Services)
	watchlist := cache.NewListWatchFromClient(client, "appfolders", api.NamespaceAll, fields.Everything())
	fmt.Println("depart")
	fmt.Println(watchlist)
	fmt.Println("fin")
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	//	resyncPeriod := 30 * time.Minute

	//Setup an informer to call functions when the watchlist changes
	informer := cache.NewSharedIndexInformer(
		watchlist,
		&crv1.AppFolder{},
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
		clientset:    clientkub,
		informer:     informer,
		queue:        queue,
		eventHandler: eventHandler,
	}
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
	config    *config.Config
	clientkub kubernetes.Interface
}

// Init initializes handler configuration
// Do nothing for default handler
func (d *Default) Init(conf *config.Config, clientb kubernetes.Interface) error {
	d.config = conf
	d.clientkub = clientb
	return nil

}

func (d *Default) ObjectCreated(obj interface{}) {
	fmt.Println("Processing change to ObjectCreated %s", obj)
	//deploymentsClient := d.clientkub.AppsV1beta2().Deployments(v1.NamespaceDefault)
	fmt.Println(reflect.TypeOf(obj))
	objAppFolder, ok := obj.(*crv1.AppFolder)
	if ok && objAppFolder != nil && reflect.TypeOf(obj).String() == "*v1.AppFolder" {
		for i, v := range obj.(*crv1.AppFolder).Spec.List.Items {

			ata, _ := json.Marshal(v)
			new := extv1beta1.Deployment{}
			json.Unmarshal(ata, &new)
			fmt.Println("objet")
			fmt.Println(d)
			fmt.Println(d.clientkub)
			fmt.Println(new.Kind)
			if strings.Compare(new.Kind, "Deployment") == 0 {
				deploymentsClient := d.clientkub.ExtensionsV1beta1().Deployments("default")
				result, err := deploymentsClient.Create(&new)
				if err != nil {
					fmt.Printf("Created deployment %q.\n", err)
					continue
				}
				fmt.Printf("Created deployment %q.\n", result.GetObjectMeta().GetName())
			}
			/*data, statusok := json.Marshal(v)
			var objmap map[string]*json.RawMessage
			json.Unmarshal(data, &objmap)
			objet := v.Object
			fmt.Println("objet")
			fmt.Println("objet")
			fmt.Println("objet")
			fmt.Println("objet")
			fmt.Println("objet")
			fmt.Println("objet")
			fmt.Println(v.Raw)
			fmt.Println(objmap)
			fmt.Println(objmap["kind"])
			fmt.Println(data)
			fmt.Println(statusok)
			fmt.Println(json.Marshal(v.Raw))
			fmt.Println(objet)
			fmt.Println("Deploy %s", objet.GetObjectKind().GroupVersionKind().Kind)
			if objet.GetObjectKind().GroupVersionKind().Kind == "Deployment" {
				fmt.Println("YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY")
				fmt.Println("YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY")
				fmt.Println("YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY")
				fmt.Println("YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY")
				deploymentsClient := d.clientkub.AppsV1beta2().Deployments(v1.NamespaceDefault)
				data, _ := json.Marshal(v)
				new := v1beta2.Deployment{}
				err := json.Unmarshal(data, &new)

				result, err := deploymentsClient.Create(&new)
				if err != nil {
					panic(err)
				}
				fmt.Printf("Created deployment %q.\n", result.GetObjectMeta().GetName())
			}*/
			fmt.Println("Array Loop", i, v)
		}
	}
	// index is the index where we are
	// element is the element from someSlice for where we are
	//result, err := deploymentsClient.Create(obj.(crv1.AppFolder).Spec.List.Items[0].Object)
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Printf("Created deployment %q.\n", result.GetObjectMeta().GetName())

}

func (d *Default) ObjectDeleted(obj interface{}) {

	fmt.Println("Processing change to ObjectCreated %s", obj)
}

func (d *Default) ObjectUpdated(oldObj, newObj interface{}) {
	fmt.Println("Processing change to ObjectCreated %s", newObj)
}
