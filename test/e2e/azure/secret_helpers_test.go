package azure

import (
	"context"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// getSecretKey returns the decoded value of key in the named Secret.
func getSecretKey(namespace, secretName, key string) (string, error) {
	secret, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	value, ok := secret.Data[key]
	if !ok {
		return "", apierrors.NewNotFound(corev1.Resource("secret"), secretName+"/"+key)
	}
	return string(value), nil
}

// getSecretLabel returns the value of label on the named Secret.
func getSecretLabel(namespace, secretName, label string) (string, error) {
	secret, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return secret.Labels[label], nil
}

// secretOwnerReferenceCount returns len(secret.metadata.ownerReferences).
func secretOwnerReferenceCount(namespace, secretName string) (int, error) {
	secret, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return 0, err
	}
	return len(secret.OwnerReferences), nil
}

// waitForSecretOwnerCount polls until secretName in namespace has exactly
// want ownerReferences, matching azure.bats's compare_owner_count helper.
func waitForSecretOwnerCount(namespace, secretName string, want int) {
	Eventually(func() (int, error) {
		return secretOwnerReferenceCount(namespace, secretName)
	}, pollTimeout, pollInterval).Should(Equal(want), "secret %s/%s ownerReferences did not converge to %d", namespace, secretName, want)
}

// waitForSecretDeleted polls until secretName in namespace is gone,
// matching azure.bats's check_secret_deleted helper.
func waitForSecretDeleted(namespace, secretName string) {
	Eventually(func() error {
		_, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
		return err
	}, pollTimeout, pollInterval).Should(Satisfy(apierrors.IsNotFound), "secret %s/%s was not deleted", namespace, secretName)
}
