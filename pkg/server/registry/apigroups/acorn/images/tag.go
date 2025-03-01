package images

import (
	"context"
	"fmt"
	"strings"

	apiv1 "github.com/acorn-io/acorn/pkg/apis/api.acorn.io/v1"
	v1 "github.com/acorn-io/acorn/pkg/apis/internal.acorn.io/v1"
	"github.com/acorn-io/baaah/pkg/router"
	"github.com/acorn-io/mink/pkg/stores"
	"github.com/acorn-io/mink/pkg/types"
	"github.com/acorn-io/mink/pkg/validator"
	"github.com/google/go-containerregistry/pkg/name"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewTagStorage(c client.WithWatch) rest.Storage {
	return stores.NewBuilder(c.Scheme(), &apiv1.ImageTag{}).
		WithValidateName(validator.NoValidation).
		WithCreate(&TagStrategy{
			client: c,
		}).Build()
}

type TagStrategy struct {
	client client.WithWatch
}

func (t *TagStrategy) Create(ctx context.Context, obj types.Object) (types.Object, error) {
	var (
		opts  = obj.(*apiv1.ImageTag)
		image *v1.ImageInstance
		err   error
	)

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		image, err = t.ImageTag(ctx, obj.GetNamespace(), obj.GetName(), opts.Tag)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &apiv1.ImageTag{
		ObjectMeta: metav1.ObjectMeta{
			Name:      image.Name,
			Namespace: image.Namespace,
		},
		Tag: opts.Tag,
	}, nil
}

func (t *TagStrategy) New() types.Object {
	return &apiv1.ImageTag{}
}

func (t *TagStrategy) ImageTag(ctx context.Context, namespace, imageName string, tagToAdd string) (*v1.ImageInstance, error) {
	image := &v1.ImageInstance{}
	err := t.client.Get(ctx, router.Key(namespace, imageName), image)
	if err != nil {
		return nil, err
	}

	imageList := &v1.ImageInstanceList{}
	err = t.client.List(ctx, imageList, &client.ListOptions{
		Namespace: namespace,
	})
	if err != nil {
		return nil, err
	}

	res, err := normalizeTags(image.Tags, false) // normalize tags *without* adding :latest
	if err != nil {
		return nil, err
	}
	set := sets.NewString(res...)

	imageRef, err := name.ParseReference(tagToAdd, name.WithDefaultRegistry(""))
	if err != nil {
		return nil, err
	}
	if _, ok := imageRef.(name.Digest); ok {
		// digest-only references are inserted without the digest portion (only registry/repository)
		// and *only* if no tagged reference with the same registry/repository prefix exists
		for _, tag := range set.List() {
			if strings.HasPrefix(tag, imageRef.Context().Name()) {
				return image, nil
			}
		}
		set.Insert(imageRef.Context().Name())
	} else {
		if set.Has(imageRef.Context().Name()) {
			// we're inserting a tag, so we can remove any repo-only entry
			set.Delete(imageRef.Context().Name())
		}
		set.Insert(imageRef.Name())

		// remove the tag from any other image (not if it's a repo-only reference, e.g. pulled by digest, as those can be duplicated)
		hasChanged := false
		for _, img := range imageList.Items {
			if img.Name == image.Name {
				continue
			}
			res, err = normalizeTags(img.Tags, false)
			if err != nil {
				return nil, err
			}
			for i, tag := range res {
				if set.Has(tag) {
					img.Tags = append(img.Tags[:i], img.Tags[i+1:]...)
					hasChanged = true
				}
			}
			if hasChanged {
				err = t.client.Update(ctx, &img)
				if err != nil {
					return nil, err
				}
				hasChanged = false
			}
		}
	}

	image.Tags = set.List()
	return image, t.client.Update(ctx, image)
}

func normalizeTags(tags []string, implicitLatestTag bool) ([]string, error) {
	var result []string
	for _, tag := range tags {
		nameOpts := []name.Option{
			name.WithDefaultRegistry(""),
		}
		if !implicitLatestTag {
			nameOpts = append(nameOpts, name.WithDefaultTag(""))
		}

		imageParsedTag, err := name.NewTag(tag, nameOpts...)
		if err != nil {
			return nil, err
		}
		if imageParsedTag.TagStr() == "" {
			result = append(result, fmt.Sprintf("%s/%s", imageParsedTag.RegistryStr(), imageParsedTag.RepositoryStr()))
		} else {
			result = append(result, imageParsedTag.Name())
		}
	}
	return result, nil
}
