package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/DoodleScheduling/swagger-hub-controller/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func needExactSpecificationStatus(reconciledInstance *v1beta1.SwaggerSpecification, expectedStatus *v1beta1.SwaggerSpecificationStatus) error {
	var expectedConditions []string
	var currentConditions []string

	for _, expectedCondition := range expectedStatus.Conditions {
		expectedConditions = append(expectedConditions, expectedCondition.Type)
		var hasCondition bool
		for _, condition := range reconciledInstance.Status.Conditions {
			if expectedCondition.Type == condition.Type {
				hasCondition = true

				if expectedCondition.Status != condition.Status {
					return fmt.Errorf("condition %s does not match expected status %s, current status=%s; current conditions=%#v", expectedCondition.Type, expectedCondition.Status, condition.Status, reconciledInstance.Status.Conditions)
				}
				if expectedCondition.Reason != condition.Reason {
					return fmt.Errorf("condition %s does not match expected reason %s, current reason=%s; current conditions=%#v", expectedCondition.Type, expectedCondition.Reason, condition.Reason, reconciledInstance.Status.Conditions)
				}
				if expectedCondition.Message != condition.Message {
					return fmt.Errorf("condition %s does not match expected message %s, current status=%s; current conditions=%#v", expectedCondition.Type, expectedCondition.Message, condition.Message, reconciledInstance.Status.Conditions)
				}
			}
		}

		if !hasCondition {
			return fmt.Errorf("missing condition %s", expectedCondition.Type)
		}
	}

	for _, condition := range reconciledInstance.Status.Conditions {
		currentConditions = append(currentConditions, condition.Type)
	}

	if len(expectedConditions) != len(currentConditions) {
		return fmt.Errorf("expected conditions %#v do not match, current conditions=%#v", expectedConditions, currentConditions)
	}

	return nil
}

var _ = Describe("SwaggerSpecification controller", func() {
	const (
		timeout  = time.Second * 4
		interval = time.Millisecond * 200
	)

	var eventuallyMatchExactConditions = func(ctx context.Context, instanceLookupKey types.NamespacedName, reconciledInstance *v1beta1.SwaggerSpecification, expectedStatus *v1beta1.SwaggerSpecificationStatus) {
		Eventually(func() error {
			err := k8sClient.Get(ctx, instanceLookupKey, reconciledInstance)
			if err != nil {
				return err
			}

			return needExactSpecificationStatus(reconciledInstance, expectedStatus)
		}, timeout, interval).Should(BeNil())
	}

	When("reconciling a suspended SwaggerSpecification", func() {
		specificationName := fmt.Sprintf("specification-%s", randStringRunes(5))

		It("should not update the status", func() {
			By("creating a new SwaggerSpecification")
			ctx := context.Background()

			gi := &v1beta1.SwaggerSpecification{
				ObjectMeta: metav1.ObjectMeta{
					Name:      specificationName,
					Namespace: "default",
				},
				Spec: v1beta1.SwaggerSpecificationSpec{
					Suspend: true,
				},
			}
			Expect(k8sClient.Create(ctx, gi)).Should(Succeed())

			By("waiting for the reconciliation")
			instanceLookupKey := types.NamespacedName{Name: specificationName, Namespace: "default"}
			reconciledInstance := &v1beta1.SwaggerSpecification{}

			eventuallyMatchExactConditions(ctx, instanceLookupKey, reconciledInstance, &v1beta1.SwaggerSpecificationStatus{})
		})
	})

	When("it reconciles a specification without spec definitions", func() {
		specificationName := fmt.Sprintf("specification-%s", randStringRunes(5))
		var specification *v1beta1.SwaggerSpecification

		It("creates a new specification", func() {
			ctx := context.Background()

			specification = &v1beta1.SwaggerSpecification{
				ObjectMeta: metav1.ObjectMeta{
					Name:      specificationName,
					Namespace: "default",
				},
				Spec: v1beta1.SwaggerSpecificationSpec{
					Servers: v1beta1.Servers{
						{
							URL: "http://api",
						},
					},
					Info: v1beta1.Info{
						Title: "foo",
					},
				},
			}
			Expect(k8sClient.Create(ctx, specification)).Should(Succeed())
		})

		It("should create a new swagger specification configmap", func() {
			ctx := context.Background()
			key := types.NamespacedName{Name: fmt.Sprintf("swagger-specification-%s", specificationName), Namespace: "default"}
			cm := &corev1.ConfigMap{}

			Eventually(func() error {
				return k8sClient.Get(ctx, key, cm)
			}, timeout, interval).Should(BeNil())

			expected := `{"components":{},"info":{"contact":{},"license":{"name":""},"title":"foo","version":""},"openapi":"3.0.1","paths":{},"servers":[{"url":"http://api"}]}`
			Expect(string(cm.BinaryData["specification.json"])).Should(Equal(expected))
		})

		It("should update the specification status", func() {
			ctx := context.Background()
			instanceLookupKey := types.NamespacedName{Name: specificationName, Namespace: "default"}
			reconciledInstance := &v1beta1.SwaggerSpecification{}

			expectedStatus := &v1beta1.SwaggerSpecificationStatus{
				ObservedGeneration: 1,
				Conditions: []metav1.Condition{
					{
						Type:    v1beta1.ConditionReady,
						Status:  metav1.ConditionTrue,
						Reason:  "ReconciliationSuccessful",
						Message: fmt.Sprintf("configmap/swagger-specification-%s created", specificationName),
					},
				},
			}
			eventuallyMatchExactConditions(ctx, instanceLookupKey, reconciledInstance, expectedStatus)
			Expect(len(reconciledInstance.Status.SubResourceCatalog)).Should(Equal(0))
		})

		It("cleans up", func() {
			ctx := context.Background()
			Expect(k8sClient.Delete(ctx, specification)).Should(Succeed())
		})
	})

	When("a definition requires basic auth", func() {
		var (
			specification *v1beta1.SwaggerSpecification
			definition    *v1beta1.SwaggerDefinition
			secret        *corev1.Secret
			server        *httptest.Server
			scope         = randStringRunes(5)
			name          = fmt.Sprintf("basicauth-%s", randStringRunes(5))
		)

		It("fetches the definition using the credentials from the referenced secret", func() {
			ctx := context.Background()

			By("serving a definition behind basic auth over https")
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				username, password, ok := r.BasicAuth()
				if !ok || username != "user" || password != "pass" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"openapi":"3.0.1","info":{"title":"secured","version":"1"},"paths":{"/secured":{"get":{"responses":{"200":{"description":"ok"}}}}}}`))
			}))

			testHTTPClient.set(server.Client())

			By("creating the credentials secret")
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("user"),
					"password": []byte("pass"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("creating a SwaggerDefinition referencing the secret")
			definition = &v1beta1.SwaggerDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
					Labels:    map[string]string{"scope": scope},
				},
				Spec: v1beta1.SwaggerDefinitionSpec{
					URL: &server.URL,
					Auth: &v1beta1.DefinitionAuth{
						Basic: &v1beta1.BasicAuth{
							SecretRef: v1beta1.LocalSecretReference{
								Name: name,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, definition)).Should(Succeed())

			specification = &v1beta1.SwaggerSpecification{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Spec: v1beta1.SwaggerSpecificationSpec{
					Info: v1beta1.Info{
						Title: "secured",
					},
					DefinitionSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"scope": scope},
					},
				},
			}
			Expect(k8sClient.Create(ctx, specification)).Should(Succeed())

			By("waiting for the merged specification")
			key := types.NamespacedName{Name: fmt.Sprintf("swagger-specification-%s", name), Namespace: "default"}
			cm := &corev1.ConfigMap{}

			Eventually(func() string {
				if err := k8sClient.Get(ctx, key, cm); err != nil {
					return ""
				}

				return string(cm.BinaryData["specification.json"])
			}, timeout, interval).Should(ContainSubstring(`"/secured"`))

			reconciledInstance := &v1beta1.SwaggerSpecification{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, reconciledInstance)).Should(Succeed())
			Expect(reconciledInstance.Status.SubResourceCatalog).Should(HaveLen(1))
			Expect(reconciledInstance.Status.SubResourceCatalog[0].Error).Should(BeEmpty())
		})

		It("cleans up", func() {
			ctx := context.Background()
			server.Close()
			testHTTPClient.set(http.DefaultClient)
			Expect(k8sClient.Delete(ctx, specification)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, definition)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	When("a definition with basic auth points to an insecure url", func() {
		var (
			specification *v1beta1.SwaggerSpecification
			definition    *v1beta1.SwaggerDefinition
			scope         = randStringRunes(5)
			name          = fmt.Sprintf("insecure-%s", randStringRunes(5))
			url           = "http://insecure/openapi"
		)

		It("does not send the credentials and reports an error", func() {
			ctx := context.Background()

			definition = &v1beta1.SwaggerDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
					Labels:    map[string]string{"scope": scope},
				},
				Spec: v1beta1.SwaggerDefinitionSpec{
					URL: &url,
					Auth: &v1beta1.DefinitionAuth{
						Basic: &v1beta1.BasicAuth{
							SecretRef: v1beta1.LocalSecretReference{
								Name: name,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, definition)).Should(Succeed())

			specification = &v1beta1.SwaggerSpecification{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Spec: v1beta1.SwaggerSpecificationSpec{
					Info: v1beta1.Info{
						Title: "insecure",
					},
					DefinitionSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"scope": scope},
					},
				},
			}
			Expect(k8sClient.Create(ctx, specification)).Should(Succeed())

			instanceLookupKey := types.NamespacedName{Name: name, Namespace: "default"}
			reconciledInstance := &v1beta1.SwaggerSpecification{}

			Eventually(func() string {
				if err := k8sClient.Get(ctx, instanceLookupKey, reconciledInstance); err != nil {
					return ""
				}

				if len(reconciledInstance.Status.SubResourceCatalog) != 1 {
					return ""
				}

				return reconciledInstance.Status.SubResourceCatalog[0].Error
			}, timeout, interval).Should(ContainSubstring("refusing to send basic auth credentials to an insecure http:// url"))
		})

		It("cleans up", func() {
			ctx := context.Background()
			Expect(k8sClient.Delete(ctx, specification)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, definition)).Should(Succeed())
		})
	})

	When("a definition with basic auth allows an insecure url", func() {
		var (
			specification *v1beta1.SwaggerSpecification
			definition    *v1beta1.SwaggerDefinition
			secret        *corev1.Secret
			server        *httptest.Server
			scope         = randStringRunes(5)
			name          = fmt.Sprintf("allowinsecure-%s", randStringRunes(5))
		)

		It("sends the credentials over http", func() {
			ctx := context.Background()

			By("serving a definition behind basic auth over http")
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				username, password, ok := r.BasicAuth()
				if !ok || username != "user" || password != "pass" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"openapi":"3.0.1","info":{"title":"insecure","version":"1"},"paths":{"/insecure":{"get":{"responses":{"200":{"description":"ok"}}}}}}`))
			}))

			By("creating the credentials secret")
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("user"),
					"password": []byte("pass"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			definition = &v1beta1.SwaggerDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
					Labels:    map[string]string{"scope": scope},
				},
				Spec: v1beta1.SwaggerDefinitionSpec{
					URL: &server.URL,
					Auth: &v1beta1.DefinitionAuth{
						Basic: &v1beta1.BasicAuth{
							AllowInsecure: true,
							SecretRef: v1beta1.LocalSecretReference{
								Name: name,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, definition)).Should(Succeed())

			specification = &v1beta1.SwaggerSpecification{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Spec: v1beta1.SwaggerSpecificationSpec{
					Info: v1beta1.Info{
						Title: "insecure",
					},
					DefinitionSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"scope": scope},
					},
				},
			}
			Expect(k8sClient.Create(ctx, specification)).Should(Succeed())

			By("waiting for the merged specification")
			key := types.NamespacedName{Name: fmt.Sprintf("swagger-specification-%s", name), Namespace: "default"}
			cm := &corev1.ConfigMap{}

			Eventually(func() string {
				if err := k8sClient.Get(ctx, key, cm); err != nil {
					return ""
				}

				return string(cm.BinaryData["specification.json"])
			}, timeout, interval).Should(ContainSubstring(`"/insecure"`))
		})

		It("cleans up", func() {
			ctx := context.Background()
			server.Close()
			Expect(k8sClient.Delete(ctx, specification)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, definition)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	When("a definition with basic auth configures a static username", func() {
		var (
			specification *v1beta1.SwaggerSpecification
			definition    *v1beta1.SwaggerDefinition
			secret        *corev1.Secret
			server        *httptest.Server
			scope         = randStringRunes(5)
			name          = fmt.Sprintf("staticuser-%s", randStringRunes(5))
		)

		It("uses the static username and the password from the referenced secret", func() {
			ctx := context.Background()

			By("serving a definition behind basic auth over https")
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				username, password, ok := r.BasicAuth()
				if !ok || username != "actuator" || password != "pass" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"openapi":"3.0.1","info":{"title":"static","version":"1"},"paths":{"/static":{"get":{"responses":{"200":{"description":"ok"}}}}}}`))
			}))

			testHTTPClient.set(server.Client())

			By("creating a secret which only holds the password")
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"password": []byte("pass"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			definition = &v1beta1.SwaggerDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
					Labels:    map[string]string{"scope": scope},
				},
				Spec: v1beta1.SwaggerDefinitionSpec{
					URL: &server.URL,
					Auth: &v1beta1.DefinitionAuth{
						Basic: &v1beta1.BasicAuth{
							Username: "actuator",
							SecretRef: v1beta1.LocalSecretReference{
								Name: name,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, definition)).Should(Succeed())

			specification = &v1beta1.SwaggerSpecification{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Spec: v1beta1.SwaggerSpecificationSpec{
					Info: v1beta1.Info{
						Title: "static",
					},
					DefinitionSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"scope": scope},
					},
				},
			}
			Expect(k8sClient.Create(ctx, specification)).Should(Succeed())

			By("waiting for the merged specification")
			key := types.NamespacedName{Name: fmt.Sprintf("swagger-specification-%s", name), Namespace: "default"}
			cm := &corev1.ConfigMap{}

			Eventually(func() string {
				if err := k8sClient.Get(ctx, key, cm); err != nil {
					return ""
				}

				return string(cm.BinaryData["specification.json"])
			}, timeout, interval).Should(ContainSubstring(`"/static"`))
		})

		It("cleans up", func() {
			ctx := context.Background()
			server.Close()
			testHTTPClient.set(http.DefaultClient)
			Expect(k8sClient.Delete(ctx, specification)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, definition)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	When("a definition declares tags", func() {
		var (
			specification *v1beta1.SwaggerSpecification
			definition    *v1beta1.SwaggerDefinition
			server        *httptest.Server
			scope         = randStringRunes(5)
			name          = fmt.Sprintf("tagged-%s", randStringRunes(5))
		)

		It("prefixes tag names and preserves their descriptions", func() {
			ctx := context.Background()

			By("serving a definition with top-level tags and tagged operations")
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"openapi":"3.0.1",
					"info":{"title":"tagged","version":"1"},
					"tags":[{"name":"task-controller","description":"Task operations"}],
					"paths":{
						"/tasks":{"get":{"tags":["task-controller"],"responses":{"200":{"description":"ok"}}}},
						"/untagged":{"get":{"responses":{"200":{"description":"ok"}}}}
					}
				}`))
			}))

			definition = &v1beta1.SwaggerDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
					Labels:    map[string]string{"scope": scope},
				},
				Spec: v1beta1.SwaggerDefinitionSpec{
					URL: &server.URL,
				},
			}
			Expect(k8sClient.Create(ctx, definition)).Should(Succeed())

			specification = &v1beta1.SwaggerSpecification{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Spec: v1beta1.SwaggerSpecificationSpec{
					Info: v1beta1.Info{
						Title: "tagged",
					},
					DefinitionSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"scope": scope},
					},
				},
			}
			Expect(k8sClient.Create(ctx, specification)).Should(Succeed())

			By("waiting for the merged specification")
			key := types.NamespacedName{Name: fmt.Sprintf("swagger-specification-%s", name), Namespace: "default"}
			cm := &corev1.ConfigMap{}

			Eventually(func() string {
				if err := k8sClient.Get(ctx, key, cm); err != nil {
					return ""
				}

				return string(cm.BinaryData["specification.json"])
			}, timeout, interval).Should(ContainSubstring(`"/tasks"`))

			var merged struct {
				Tags []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"tags"`
				Paths map[string]struct {
					Get struct {
						Tags []string `json:"tags"`
					} `json:"get"`
				} `json:"paths"`
			}
			Expect(json.Unmarshal(cm.BinaryData["specification.json"], &merged)).Should(Succeed())

			expectedTag := fmt.Sprintf("%s.task-controller", name)

			By("prefixing the top-level tag name while keeping its description")
			Expect(merged.Tags).Should(ContainElement(HaveField("Name", expectedTag)))
			Expect(merged.Tags).Should(ContainElement(HaveField("Description", "Task operations")))

			By("prefixing the tag referenced by the tagged operation")
			Expect(merged.Paths["/tasks"].Get.Tags).Should(ConsistOf(expectedTag))

			By("falling back to the definition name for untagged operations")
			Expect(merged.Paths["/untagged"].Get.Tags).Should(ConsistOf(name))
		})

		It("cleans up", func() {
			ctx := context.Background()
			server.Close()
			Expect(k8sClient.Delete(ctx, specification)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, definition)).Should(Succeed())
		})
	})
})
