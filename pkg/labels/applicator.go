package labels

import (
	"errors"

	"github.com/kubernetes/kubernetes/pkg/labels"
)

// the type of the object being labeled.
type Type string

func (l Type) String() string {
	return string(l)
}

var (
	POD  = Type("pod")
	NODE = Type("node")
)

var InvalidType error = errors.New("Invalid type provided")

func AsType(v string) (Type, error) {
	switch v {
	case POD.String():
		return POD, nil
	case NODE.String():
		return NODE, nil
	default:
		return Type(""), InvalidType
	}
}

type Labeled struct {
	LabelType Type
	ID        string
	Labels    labels.Set
}

func (l Labeled) SameAs(o Labeled) bool {
	return l.ID == o.ID && l.LabelType == o.LabelType
}

// General purpose backing store for labels and assignment
// to P2 objects
type Applicator interface {

	// Assign a label to the object identified by the Type and ID
	SetLabel(labelType Type, id, name, value string) error

	// Remove a label from the identified object
	RemoveLabel(labelType Type, id, name string) error

	// Get all Labels assigned to the given object
	GetLabels(labelType Type, id string) (labels.Set, error)

	// Return all objects that match the given selector
	GetMatches(selector labels.LabelSelector) ([]Labeled, error)
}
