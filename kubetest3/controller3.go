package main

// increase replica or create a pod
// record exact time for this
import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

func createAPod(dynamicClient dynamic.Interface, namespace string, deploymentName string) bool {
	//input- deployment string
	//kubeconfig
	//dynamics client
	//meterics client

	//if function returns true. then pod definitely created. but could be in pending state.
	//for instance, if there's a limit on pods and only 3 pods possible, one of the three is in terminating state
	//this function is called, a pod will be created (podscheduled -> ready -> pending) but won't change its state to "running" until the other pod is cleared

	deploymentRes := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	deployment, err := dynamicClient.Resource(deploymentRes).Namespace(namespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("error getting deployment: %s\n", err.Error())
		return false
	}

	replicas := deployment.Object["spec"].(map[string]interface{})["replicas"].(int64)
	replicas++
	deployment.Object["spec"].(map[string]interface{})["replicas"] = replicas

	_, err = dynamicClient.Resource(deploymentRes).Namespace(namespace).Update(context.TODO(), deployment, metav1.UpdateOptions{})
	if err != nil {
		log.Fatalf("error updating: %s\n", err.Error())
		return false
	}
	fmt.Println("increased replicas to", replicas)
	return true

}

func getListOfPods(metricsClient versioned.Interface, namespace string) (*v1beta1.PodMetricsList, error) {

	podMetricsList, err := metricsClient.MetricsV1beta1().PodMetricses(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatalf("error listing pod metrics: %s\n", err.Error())

		return nil, err
	}
	return podMetricsList, nil

}

func getPods(dynamicClient dynamic.Interface, namespace string) (*unstructured.UnstructuredList, error) {

	podResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	podList, err := dynamicClient.Resource(podResource).Namespace(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatalf("error getting pods: %s\n", err.Error())
		return nil, err
	}

	return podList, nil
}

func getPodStartUpTime(dynamicClient dynamic.Interface, namespace string, deploymentName string, podName string) (time.Duration, error) {
	//input

	//output: time difference between "Ready" and "PodScheduled" in seconds. More precision not possible via kubectl/go-client
	//more info in doc
	var duration time.Duration
	podResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	pod, err := dynamicClient.Resource(podResource).Namespace(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("error getting pod: %s\n", err.Error())
		return duration, err
	}

	conditions, found, err := unstructured.NestedSlice(pod.Object, "status", "conditions")
	if err != nil || !found {
		log.Fatalf("error retrieving pod conditions: %s\n", err.Error())
		return duration, err
	}

	var podScheduledTime, podReadyTime *time.Time
	for _, condition := range conditions {
		conditionMap := condition.(map[string]interface{})
		conditionType, found := conditionMap["type"].(string)
		if !found {
			continue
		}
		lastTransitionTime, found := conditionMap["lastTransitionTime"].(string)
		if !found {
			continue
		}
		parsedTime, err := time.Parse(time.RFC3339, lastTransitionTime)
		if err != nil {
			log.Fatalf("error parsing time: %s\n", err.Error())
			return duration, err
		}

		if conditionType == "PodScheduled" {
			podScheduledTime = &parsedTime
		}

		if conditionType == "Ready" {
			podReadyTime = &parsedTime
		}

	}

	if podScheduledTime == nil {
		log.Fatalf("pod has no PodScheduled condition")
		return duration, nil
	}

	if podReadyTime == nil {
		log.Fatalf("pod has no Ready condition")
		return duration, nil
	}

	duration = podReadyTime.Sub(*podScheduledTime)
	fmt.Printf("time diff between PodScheduled and Ready: %v\n", duration)
	return duration, nil

}

func getDeploymentFromReplicaSet(dynamicClient dynamic.Interface, namespace string, replicaSetName string) (string, error) {

	replicaSetResource := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}

	//get replica set
	replicaSet, err := dynamicClient.Resource(replicaSetResource).Namespace(namespace).Get(context.TODO(), replicaSetName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	//looking through yaml file
	ownerReferences, found, err := unstructured.NestedSlice(replicaSet.Object, "metadata", "ownerReferences")
	if err != nil || !found {
		return "", fmt.Errorf("owner references not found for ReplicaSet %s", replicaSetName)
	}

	for _, ownerReference := range ownerReferences {
		ownerRefMap := ownerReference.(map[string]interface{})
		if ownerRefMap["kind"] == "Deployment" {
			return ownerRefMap["name"].(string), nil
		}
	}

	return "", fmt.Errorf("deployment owner reference not found for ReplicaSet %s", replicaSetName)
}

func main() {
	var kubeconfig *string

	//this is for authentication and authorization?
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", home+"/.kube/config", "kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "kubeconfig file")
	}

	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("error building kubeconfig: %s\n", err.Error())
	}

	//dynamic client to interact with resources(pods, deployments etc)
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create dynamic client: %s\n", err.Error())
	}

	//metrics client to get metrics
	metricsClient, err := versioned.NewForConfig(config) //versioned.Interface
	if err != nil {
		log.Fatalf("Failed to create metrics client: %s\n", err.Error())
	}
	if metricsClient != nil {
		fmt.Printf("created metrics client")
		//dummy code
	}

	namespace := "default"
	deploymentName := "efficientdetnet-deployment"

	//create a pod
	isPodCreated := createAPod(dynamicClient, namespace, deploymentName)
	if isPodCreated {
		fmt.Printf("pod created successfully")
	}

	//get list of pod metrics
	//podMetricsList, err := getListOfPods(metricsClient, namespace)

	//get pods
	podList, err := getPods(dynamicClient, namespace)
	if podList.IsList() {

		// get the startup times of ALL pods can add filters to get for a specific deployment
		for _, pod := range podList.Items {

			podName := pod.GetName()
			podNamespace := pod.GetNamespace() //default
			var podDeploymentName string

			ownerReferences, found, err := unstructured.NestedSlice(pod.Object, "metadata", "ownerReferences")
			if err != nil || !found {
				fmt.Println("Owner References: <not found>")
			} else {
				for _, ownerReference := range ownerReferences {
					ownerRefMap := ownerReference.(map[string]interface{})
					if ownerRefMap["kind"] == "ReplicaSet" {
						replicaSetName := ownerRefMap["name"].(string)
						podDeploymentName, err = getDeploymentFromReplicaSet(dynamicClient, podNamespace, replicaSetName)
						if err != nil {
							fmt.Printf("Error getting deployment name from ReplicaSet %s: %s\n", replicaSetName, err.Error())
						}
						break
					}
				}
			}

			getPodStartUpTime(dynamicClient, podNamespace, podDeploymentName, podName) //not returning

		}

	}

	//podName := "efficientdetnet-deployment-7559b4b9cd-k76mr"
	//podNamespace := "default"

}
