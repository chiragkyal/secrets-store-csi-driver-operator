package operator

import (
	"fmt"

	opv1 "github.com/openshift/api/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
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

type fakeCSIDriverLister struct {
	driver *storagev1.CSIDriver
}

func newFakeCSIDriverLister(driver *storagev1.CSIDriver) *fakeCSIDriverLister {
	return &fakeCSIDriverLister{driver: driver}
}

func (f *fakeCSIDriverLister) List(_ labels.Selector) ([]*storagev1.CSIDriver, error) {
	if f.driver == nil {
		return nil, nil
	}
	return []*storagev1.CSIDriver{f.driver}, nil
}

func (f *fakeCSIDriverLister) Get(name string) (*storagev1.CSIDriver, error) {
	if f.driver != nil && f.driver.Name == name {
		return f.driver, nil
	}
	return nil, fmt.Errorf("not found: %s", name)
}
