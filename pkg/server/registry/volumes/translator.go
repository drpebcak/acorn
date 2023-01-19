package volumes

import (
	"context"
	"strings"

	apiv1 "github.com/acorn-io/acorn/pkg/apis/api.acorn.io/v1"
	v1 "github.com/acorn-io/acorn/pkg/apis/internal.acorn.io/v1"
	"github.com/acorn-io/acorn/pkg/labels"
	"github.com/acorn-io/mink/pkg/types"
	corev1 "k8s.io/api/core/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/storage"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type Translator struct {
	c kclient.Client
}

func (t *Translator) FromPublicName(ctx context.Context, namespace, name string) (string, string, error) {
	return "", name, nil
}

func (t *Translator) ListOpts(ctx context.Context, namespace string, opts storage.ListOptions) (string, storage.ListOptions, error) {
	sel := opts.Predicate.Label
	if sel == nil {
		sel = klabels.Everything()
	}
	req, _ := klabels.NewRequirement(labels.AcornManaged, selection.Equals, []string{"true"})
	sel = sel.Add(*req)

	if namespace != "" {
		req, _ := klabels.NewRequirement(labels.AcornAppNamespace, selection.Equals, []string{namespace})
		sel = sel.Add(*req)
	}
	opts.Predicate.Label = sel
	return "", opts, nil
}

func pvToVolume(pv corev1.PersistentVolume) *apiv1.Volume {
	var (
		accessModes      []v1.AccessMode
		shortAccessModes []string
	)

	for _, accessMode := range pv.Spec.AccessModes {
		switch accessMode {
		case corev1.ReadWriteOnce:
			accessModes = append(accessModes, v1.AccessModeReadWriteOnce)
			shortAccessModes = append(shortAccessModes, "RWO")
		case corev1.ReadOnlyMany:
			accessModes = append(accessModes, v1.AccessModeReadOnlyMany)
			shortAccessModes = append(shortAccessModes, "ROX")
		case corev1.ReadWriteMany:
			accessModes = append(accessModes, v1.AccessModeReadWriteMany)
			shortAccessModes = append(shortAccessModes, "RWX")
		}
	}

	vol := &apiv1.Volume{
		ObjectMeta: pv.ObjectMeta,
		Spec: apiv1.VolumeSpec{
			Capacity:    pv.Spec.Capacity.Storage(),
			AccessModes: accessModes,
			Class:       pv.Spec.StorageClassName,
		},
		Status: apiv1.VolumeStatus{
			AppName:      pv.Labels[labels.AcornAppName],
			AppNamespace: pv.Labels[labels.AcornAppNamespace],
			VolumeName:   pv.Labels[labels.AcornVolumeName],
			Status:       strings.ToLower(string(pv.Status.Phase)),
			Columns: apiv1.VolumeColumns{
				AccessModes: strings.Join(shortAccessModes, ","),
			},
		},
	}
	vol.UID = vol.UID + "-v"
	vol.Namespace = pv.Labels[labels.AcornAppNamespace]
	if !pv.DeletionTimestamp.IsZero() {
		vol.Status.Status += "/deleted"
	}

	return vol
}

func (t *Translator) ToPublic(ctx context.Context, objs ...runtime.Object) (result []types.Object, _ error) {
	for _, obj := range objs {
		pv := obj.(*corev1.PersistentVolume)
		result = append(result, pvToVolume(*pv))
	}
	return
}

func (t *Translator) FromPublic(ctx context.Context, obj runtime.Object) (types.Object, error) {
	vol := obj.(*apiv1.Volume)
	pv := &corev1.PersistentVolume{
		ObjectMeta: vol.ObjectMeta,
	}
	pv.UID = ktypes.UID(strings.TrimSuffix(string(pv.UID), "-v"))
	return pv, nil
}

func (t *Translator) NewPublic() types.Object {
	return &apiv1.Volume{}
}

func (t *Translator) NewPublicList() types.ObjectList {
	return &apiv1.VolumeList{}
}
