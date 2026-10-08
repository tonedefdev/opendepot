package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDeletePodUntilSuccessful(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "version-controller", Namespace: "opendepot-system"},
	})

	waitCalled := false
	deletePodUntilSuccessful(clientset, "opendepot-system", "version-controller", func() {
		waitCalled = true
	})

	if waitCalled {
		t.Fatal("wait called after successful Pod deletion")
	}
	if _, err := clientset.CoreV1().Pods("opendepot-system").Get(t.Context(), "version-controller", metav1.GetOptions{}); err == nil {
		t.Fatal("Pod still exists after deletion")
	}
}
