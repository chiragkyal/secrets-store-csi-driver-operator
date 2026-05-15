package operator

import (
	"fmt"

	opv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type fakeClusterCSIDriverLister struct {
	ccd *opv1.ClusterCSIDriver
}

func newFakeClusterCSIDriverLister(ccd *opv1.ClusterCSIDriver) *fakeClusterCSIDriverLister {
	return &fakeClusterCSIDriverLister{ccd: ccd}
}

func (f *fakeClusterCSIDriverLister) List(_ labels.Selector) ([]*opv1.ClusterCSIDriver, error) {
	if f.ccd == nil {
		return nil, nil
	}
	return []*opv1.ClusterCSIDriver{f.ccd}, nil
}

func (f *fakeClusterCSIDriverLister) Get(name string) (*opv1.ClusterCSIDriver, error) {
	if f.ccd != nil && f.ccd.Name == name {
		return f.ccd, nil
	}
	return nil, fmt.Errorf("not found: %s", name)
}
