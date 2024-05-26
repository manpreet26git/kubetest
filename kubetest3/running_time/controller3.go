package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	var kubeconfig *string

	// This is for authentication and authorization
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "kubeconfig file")
	}

	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %s\n", err.Error())
	}

	// Dynamic client to interact with resources
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create dynamic client: %s\n", err.Error())
	}

	namespace := "default"
	deploymentName := "efficientdetnet-deployment"
	deploymentRes := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	// Get the deployment
	deployment, err := dynamicClient.Resource(deploymentRes).Namespace(namespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Failed to get deployment: %s\n", err.Error())
	}

	// Increase the replica count by 1
	replicas, found, err := unstructured.NestedInt64(deployment.Object, "spec", "replicas")
	if err != nil || !found {
		log.Fatalf("Failed to get replicas: %s\n", err.Error())
	}
	replicas++
	if err := unstructured.SetNestedField(deployment.Object, replicas, "spec", "replicas"); err != nil {
		log.Fatalf("Failed to set replicas: %s\n", err.Error())
	}

	// Update the deployment
	_, err = dynamicClient.Resource(deploymentRes).Namespace(namespace).Update(context.TODO(), deployment, metav1.UpdateOptions{})
	if err != nil {
		log.Fatalf("Failed to update deployment: %s\n", err.Error())
	}
	fmt.Printf("Increased replicas to %d at: %v\n", replicas, time.Now())

	// Watch for pod events
	podResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	watcher, err := dynamicClient.Resource(podResource).Namespace(namespace).Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatalf("Error setting up watch: %s\n", err.Error())
	}
	defer watcher.Stop()

	for event := range watcher.ResultChan() {
		if event.Type == watch.Added || event.Type == watch.Modified {
			pod := event.Object.(*unstructured.Unstructured)
			if pod.GetLabels()["app"] == deploymentName { // Assuming the label matches the deployment name
				statusPhase, found, err := unstructured.NestedString(pod.Object, "status", "phase")
				if err != nil || !found {
					continue
				}

				fmt.Printf("POD_NAME: %s\n", pod.GetName())
				fmt.Printf("TIME: %s\n", time.Now().Format(time.RFC3339))
				fmt.Printf("PHASE: %s\n", statusPhase)
				fmt.Println("CUSTOM AMAZING TEXT HERE!")
				fmt.Println("---")

				if statusPhase == "Succeeded" {
					fmt.Println("----> This pod succeeded, do something here!")
					fmt.Println("---")
					break
				}
			}
		}
	}
}
