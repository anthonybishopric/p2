package labels

import (
	"path"
	"sort"

	"github.com/kubernetes/kubernetes/pkg/labels"
	"github.com/square/p2/Godeps/_workspace/src/github.com/Sirupsen/logrus"
	"github.com/square/p2/Godeps/_workspace/src/github.com/hashicorp/consul/api"
	"github.com/square/p2/pkg/logging"
)

const labelRoot = "labels"

type consulKV interface {
	List(prefix string, opts *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error)
	Delete(key string, opts *api.WriteOptions) (api.WriteMeta, error)
}

type consulApplicator struct {
	kv     consulKV
	logger logging.Logger
}

type labeled struct {
	labels    labels.Set
	labelType Type
	id        string
}

func NewConsulApplicator(client *api.Client) *consulApplicator {
	return &consulApplicator{
		logger: logging.DefaultLogger,
		kv:     client.KV(),
	}
}

func labelPath(labelType, id, label string) string {
	return path.Join(objectPath(labelType, id), label)
}

func objectPath(labelType Type, id string) string {
	return path.Join(labelRoot, labelType.String(), id)
}

func (c *consulApplicator) GetLabels(labelType Type, id string) (labels.Set, error) {
	kvRes, meta, err := c.kv.List(objectPath(labelType, id), nil)
	res := labels.Set{}
	if err != nil {
		return res, err
	}
	for _, kv := range kvRes {
		res[path.Base(kv.Key)] = string(kv.Value)
	}
	return res, nil
}

func (c *consulApplicator) GetMatches(selector labels.LabelSelector) ([]Labeled, error) {
	// TODO: Label selector result caching
	allMatches, meta, err := c.kv.List(labelRoot, nil)
	if err != nil {
		return nil, err
	}
	res := []Labeled{}
	for labeled := range c.groupLabels(allMatches) {
		if selector.Matches(labeled.Labels) {
			res = append(res, labeled)
		}
	}
	return res, nil
}

func (c *consulApplicator) RemoveLabel(labelType Type, id, label string) error {
	c.kv.labelPath(labelType, id, label)

}

// take a set of Consul kvpairs and convert it into an iterator of Labeled.
func (c *consulApplicator) groupLabels(pairs api.KVPairs) <-chan Labeled {
	ch := make(chan Labeled)
	go func() {
		var current Labeled
		sort.Sort(byPath(pairs))

		for _, pair := range pairs {
			// {LabelRoot}/{LabelType}/{ID}/{LabelKey}
			typeStr := path.Base(path.Dir(path.Dir(pair.Key)))
			objID := path.Base(path.Dir(pair.Key))
			labelKey := path.Base(pair.Key)

			labelType, err := AsType(typeStr)
			if err != nil {
				c.logger.WithErrorAndFields(err, logrus.Fields{
					"key":   pair.Key,
					"value": string(pair.Value),
				}).Errorln("Invalid label value found")
				continue
			}

			next := Labeled{
				ID:        objID,
				LabelType: labelType,
				Labels:    labels.Set{},
			}
			if !current.SameAs(next) {
				if current.ID != "" {
					ch <- current
				}
				current = next
			}
			current.Labels[labelKey] = string(pair.Value)
		}
		close(ch)
	}()
	return ch
}

type byPath api.KVPairs

func (b byPath) Len() int {
	return len(b)
}

func (b byPath) Less(i, j int) bool {
	return b[i].Key < b[j].Key
}

func (b byPath) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

var _ Applicator = &consulApplicator{}
