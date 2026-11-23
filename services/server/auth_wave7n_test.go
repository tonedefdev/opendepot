package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func wave7nAuthAPI(t *testing.T, bodies map[string]string) {
	t.Helper()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for suffix, body := range bodies {
			if strings.HasSuffix(r.URL.Path, suffix) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))

				return
			}
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(api.Close)

	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}
}

func TestWave7NGenerateKubeClientKubeconfig(t *testing.T) {
	wave6AuthGlobals(t)

	t.Run("invalid kubeconfig bytes are rejected", func(t *testing.T) {
		if _, err := generateKubeClient([]byte("{{{"), nil, false); err == nil {
			t.Fatal("expected kubeconfig parse error")
		}
	})

	t.Run("kubeconfig without a cluster is rejected", func(t *testing.T) {
		if _, err := generateKubeClient([]byte("apiVersion: v1\nkind: Config\n"), nil, false); err == nil {
			t.Fatal("expected client config error")
		}
	})

	t.Run("valid kubeconfig builds a client", func(t *testing.T) {
		cs, err := generateKubeClient([]byte(agentTestKubeconfig("https://127.0.0.1:1", "token")), nil, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cs == nil {
			t.Fatal("expected clientset")
		}
	})

	t.Run("bearer token overrides in-cluster config", func(t *testing.T) {
		wave7nAuthAPI(t, nil)

		token := "wave7n-token"
		cs, err := generateKubeClient(nil, &token, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cs == nil {
			t.Fatal("expected clientset")
		}
	})
}

func TestWave7NFindGroupBindingMalformedList(t *testing.T) {
	wave6AuthGlobals(t)
	wave7nAuthAPI(t, map[string]string{"/groupbindings": "{not json"})

	if _, err := findGroupBinding(context.Background(), wave6Clientset(t), []string{"dev"}); err == nil || !strings.Contains(err.Error(), "failed to unmarshal GroupBindingList") {
		t.Fatalf("err = %v, want unmarshal error", err)
	}
}

func TestWave7NFindSecurityGroupBinding(t *testing.T) {
	ctx := context.Background()

	t.Run("matching binding is returned", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave7nAuthAPI(t, map[string]string{
			"/securitygroupbindings": string(agentTestJSON(t, opendepotv1alpha1.SecurityGroupBindingList{
				Items: []opendepotv1alpha1.SecurityGroupBinding{{
					ObjectMeta: metav1.ObjectMeta{Name: "sec-admin"},
					Spec:       opendepotv1alpha1.SecurityGroupBindingSpec{Expression: `"admins" in groups`},
				}},
			})),
		})

		binding, err := findSecurityGroupBinding(ctx, wave6Clientset(t), []string{"admins"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if binding.Name != "sec-admin" {
			t.Fatalf("binding = %q, want sec-admin", binding.Name)
		}
	})

	t.Run("malformed list is an error", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave7nAuthAPI(t, map[string]string{"/securitygroupbindings": "{not json"})

		if _, err := findSecurityGroupBinding(ctx, wave6Clientset(t), []string{"dev"}); err == nil || !strings.Contains(err.Error(), "failed to unmarshal SecurityGroupBindingList") {
			t.Fatalf("err = %v, want unmarshal error", err)
		}
	})

	t.Run("invalid expression fails closed", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave7nAuthAPI(t, map[string]string{
			"/securitygroupbindings": string(agentTestJSON(t, opendepotv1alpha1.SecurityGroupBindingList{
				Items: []opendepotv1alpha1.SecurityGroupBinding{{
					ObjectMeta: metav1.ObjectMeta{Name: "broken"},
					Spec:       opendepotv1alpha1.SecurityGroupBindingSpec{Expression: "groups ==="},
				}},
			})),
		})

		if _, err := findSecurityGroupBinding(ctx, wave6Clientset(t), []string{"dev"}); err == nil || !strings.Contains(err.Error(), "is invalid") {
			t.Fatalf("err = %v, want invalid-expression error", err)
		}
	})

	t.Run("evaluation failure fails closed", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave7nAuthAPI(t, map[string]string{
			"/securitygroupbindings": string(agentTestJSON(t, opendepotv1alpha1.SecurityGroupBindingList{
				Items: []opendepotv1alpha1.SecurityGroupBinding{{
					ObjectMeta: metav1.ObjectMeta{Name: "bad-type"},
					Spec:       opendepotv1alpha1.SecurityGroupBindingSpec{Expression: `groups[len(groups)] == "x"`},
				}},
			})),
		})

		if _, err := findSecurityGroupBinding(ctx, wave6Clientset(t), []string{"dev"}); err == nil || !strings.Contains(err.Error(), "evaluation failed") {
			t.Fatalf("err = %v, want evaluation error", err)
		}
	})
}
