package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type SwaggerDefinition struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SwaggerDefinitionSpec `json:"spec,omitempty"`
}

// SwaggerDefinitionList contains a list of SwaggerDefinition.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SwaggerDefinitionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SwaggerDefinition `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &SwaggerDefinition{}, &SwaggerDefinitionList{})
}

// SwaggerDefinitionSpec defines the desired state of SwaggerDefinition.
// +k8s:openapi-gen=true
type SwaggerDefinitionSpec struct {
	URL *string `json:"url,omitempty"`

	// Auth configures how the controller authenticates while fetching the
	// definition from url. It is only evaluated by SwaggerSpecification which
	// fetches definitions server side.
	// +optional
	Auth *DefinitionAuth `json:"auth,omitempty"`
}

// DefinitionAuth defines the authentication used to fetch a definition.
type DefinitionAuth struct {
	// Basic configures http basic authentication.
	// +optional
	Basic *BasicAuth `json:"basic,omitempty"`
}

// BasicAuth defines http basic authentication credentials.
type BasicAuth struct {
	// Username is a static username. If set it takes precedence over the username
	// looked up from secretRef.usernameField.
	// +optional
	Username string `json:"username,omitempty"`

	// SecretRef references a secret in the same namespace as the SwaggerDefinition
	// which holds the credentials.
	// +required
	SecretRef LocalSecretReference `json:"secretRef"`

	// AllowInsecure permits sending the credentials to a plain http:// url.
	// +optional
	AllowInsecure bool `json:"allowInsecure,omitempty"`
}

// LocalSecretReference references a secret within the same namespace.
type LocalSecretReference struct {
	// Name of the secret.
	// +required
	Name string `json:"name"`

	// UsernameField is the secret key which holds the username. It is ignored if
	// a static username is configured.
	// +kubebuilder:default:=username
	// +optional
	UsernameField string `json:"usernameField,omitempty"`

	// PasswordField is the secret key which holds the password.
	// +kubebuilder:default:=password
	// +optional
	PasswordField string `json:"passwordField,omitempty"`
}
