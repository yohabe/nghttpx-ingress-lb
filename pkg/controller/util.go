/*
Copyright 2015 The Kubernetes Authors All rights reserved.

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

/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package controller

import (
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	listersnetworkingv1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/klog/v2"
)

// loadBalancerIngressesIPEqual compares a and b, and if their IP fields are equal, returns true.  a and b might not be sorted in the
// particular order.  They just compared from first to last, and if there is a difference, this function returns false.
func loadBalancerIngressesIPEqual(a, b []corev1.LoadBalancerIngress) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].IP != b[i].IP {
			return false
		}
	}

	return true
}

// sortLoadBalancerIngress sorts a by IP and Hostname in the ascending order.
func sortLoadBalancerIngress(a []corev1.LoadBalancerIngress) {
	sort.Slice(a, func(i, j int) bool {
		return a[i].IP < a[j].IP || (a[i].IP == a[j].IP && a[i].Hostname < a[j].Hostname)
	})
}

// uniqLoadBalancerIngress removes duplicated items from a.  This function assumes a is sorted by sortLoadBalancerIngress.
func uniqLoadBalancerIngress(a []corev1.LoadBalancerIngress) []corev1.LoadBalancerIngress {
	if len(a) == 0 {
		return a
	}
	p := 0
	for i := 1; i < len(a); i++ {
		if a[p].IP == a[i].IP && a[p].Hostname == a[i].Hostname {
			continue
		}
		p++
		if p != i {
			a[p] = a[i]
		}
	}

	return a[:p+1]
}

// removeAddressFromLoadBalancerIngress removes addr from a.  addr may match IP or Hostname.
func removeAddressFromLoadBalancerIngress(a []corev1.LoadBalancerIngress, addr string) []corev1.LoadBalancerIngress {
	var cnt int
	for i := range a {
		if a[i].IP == addr || a[i].Hostname == addr {
			cnt++
		}
	}

	if cnt == 0 {
		return a
	}
	if cnt == len(a) {
		return nil
	}

	dst := make([]corev1.LoadBalancerIngress, len(a)-cnt)

	p := 0
	for i := range a {
		if a[i].IP == addr || a[i].Hostname == addr {
			continue
		}
		dst[p] = a[i]
		p++
	}
	return dst
}

// podFindPort is copied from
// https://github.com/kubernetes/kubernetes/blob/886e04f1fffbb04faf8a9f9ee141143b2684ae68/pkg/api/v1/pod/util.go#L29 because original
// FindPort requires k8s.io/kubernetes/pkg/api/v1 while we use k8s.io/client-go/pkg/api/v1.

// podFindPort locates the container port for the given pod and portName.  If the targetPort is a number, use that.  If the targetPort is a
// string, look that string up in all named ports in all containers in the target pod.  If no match is found, fail.
func podFindPort(pod *corev1.Pod, svcPort *corev1.ServicePort) (int32, error) {
	portName := svcPort.TargetPort
	switch portName.Type {
	case intstr.String:
		name := portName.StrVal
	loop:
		for i := range pod.Spec.Containers {
			container := &pod.Spec.Containers[i]
			for i := range container.Ports {
				port := &container.Ports[i]
				// port.Name must be unique inside Pod.
				if port.Name == name {
					if port.Protocol == svcPort.Protocol {
						return port.ContainerPort, nil
					}
					break loop
				}
			}
		}
	case intstr.Int:
		return int32(portName.IntValue()), nil
	}

	return 0, fmt.Errorf("no suitable port for manifest: %s", pod.UID)
}

// podLabelSelector returns labels.Selector from labelSet.
func podLabelSelector(labelSet map[string]string) labels.Selector {
	l := make(map[string]string)
	// Remove labels which represent pod template hash, revision, or generation.
	for k, v := range labelSet {
		switch k {
		case appsv1.ControllerRevisionHashLabelKey:
		case "pod-template-generation": // Used by DaemonSet
		case appsv1.DefaultDeploymentUniqueLabelKey:
			continue
		}
		l[k] = v
	}
	return labels.Set(l).AsSelector()
}

// validateIngressClass checks whether this controller should process ing or not.
func validateIngressClass(ing *networkingv1.Ingress, ingressClassController string, ingClassLister listersnetworkingv1.IngressClassLister,
	requireIngressClass bool) bool {
	if ing.Spec.IngressClassName != nil {
		ingClass, err := ingClassLister.Get(*ing.Spec.IngressClassName)
		if err != nil {
			klog.Errorf("Could not get IngressClass %v: %v", *ing.Spec.IngressClassName, err)
			return false
		}
		if ingClass.Spec.Controller != ingressClassController {
			klog.V(4).Infof("Skip Ingress %v/%v which needs IngressClass %v controller %v", ing.Namespace, ing.Name,
				ingClass.Name, ingClass.Spec.Controller)
			return false
		}
		return true
	}

	if requireIngressClass {
		// Requiring IngressClass is the intended behavior of Ingress resource.  But historically, we do this differently.
		return false
	}

	// Check defaults

	ingClasses, err := ingClassLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Could not list IngressClass: %v", err)
		return false
	}

	for _, ingClass := range ingClasses {
		if ingClass.Annotations[networkingv1.AnnotationIsDefaultIngressClass] != "true" {
			continue
		}

		if ingClass.Spec.Controller != ingressClassController {
			klog.V(4).Infof("Skip Ingress %v/%v because it defaults to IngressClass %v controller %v", ing.Namespace, ing.Name,
				ingClass.Name, ingClass.Spec.Controller)
			return false
		}
		return true
	}

	// If there is no default IngressClass, process the Ingress.
	return true
}
