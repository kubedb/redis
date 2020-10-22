/*
Copyright AppsCode Inc. and Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"context"
	"fmt"

	api "kubedb.dev/apimachinery/apis/kubedb/v1alpha2"
	cs "kubedb.dev/apimachinery/client/clientset/versioned/typed/kubedb/v1alpha2"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/golang/glog"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	kutil "kmodules.xyz/client-go"
)

func CreateOrPatchPgBouncer(ctx context.Context, c cs.KubedbV1alpha2Interface, meta metav1.ObjectMeta, transform func(*api.PgBouncer) *api.PgBouncer, opts metav1.PatchOptions) (*api.PgBouncer, kutil.VerbType, error) {
	cur, err := c.PgBouncers(meta.Namespace).Get(ctx, meta.Name, metav1.GetOptions{})
	if kerr.IsNotFound(err) {
		glog.V(3).Infof("Creating PgBouncer %s/%s.", meta.Namespace, meta.Name)
		out, err := c.PgBouncers(meta.Namespace).Create(ctx, transform(&api.PgBouncer{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PgBouncer",
				APIVersion: api.SchemeGroupVersion.String(),
			},
			ObjectMeta: meta,
		}), metav1.CreateOptions{
			DryRun:       opts.DryRun,
			FieldManager: opts.FieldManager,
		})
		return out, kutil.VerbCreated, err
	} else if err != nil {
		return nil, kutil.VerbUnchanged, err
	}
	return PatchPgBouncer(ctx, c, cur, transform, opts)
}

func PatchPgBouncer(ctx context.Context, c cs.KubedbV1alpha2Interface, cur *api.PgBouncer, transform func(*api.PgBouncer) *api.PgBouncer, opts metav1.PatchOptions) (*api.PgBouncer, kutil.VerbType, error) {
	return PatchPgBouncerObject(ctx, c, cur, transform(cur.DeepCopy()), opts)
}

func PatchPgBouncerObject(ctx context.Context, c cs.KubedbV1alpha2Interface, cur, mod *api.PgBouncer, opts metav1.PatchOptions) (*api.PgBouncer, kutil.VerbType, error) {
	curJson, err := json.Marshal(cur)
	if err != nil {
		return nil, kutil.VerbUnchanged, err
	}

	modJson, err := json.Marshal(mod)
	if err != nil {
		return nil, kutil.VerbUnchanged, err
	}

	patch, err := jsonpatch.CreateMergePatch(curJson, modJson)
	if err != nil {
		return nil, kutil.VerbUnchanged, err
	}
	if len(patch) == 0 || string(patch) == "{}" {
		return cur, kutil.VerbUnchanged, nil
	}
	glog.V(3).Infof("Patching PgBouncer %s/%s with %s.", cur.Namespace, cur.Name, string(patch))
	out, err := c.PgBouncers(cur.Namespace).Patch(ctx, cur.Name, types.MergePatchType, patch, opts)
	return out, kutil.VerbPatched, err
}

func TryUpdatePgBouncer(ctx context.Context, c cs.KubedbV1alpha2Interface, meta metav1.ObjectMeta, transform func(*api.PgBouncer) *api.PgBouncer, opts metav1.UpdateOptions) (result *api.PgBouncer, err error) {
	attempt := 0
	err = wait.PollImmediate(kutil.RetryInterval, kutil.RetryTimeout, func() (bool, error) {
		attempt++
		cur, e2 := c.PgBouncers(meta.Namespace).Get(ctx, meta.Name, metav1.GetOptions{})
		if kerr.IsNotFound(e2) {
			return false, e2
		} else if e2 == nil {

			result, e2 = c.PgBouncers(cur.Namespace).Update(ctx, transform(cur.DeepCopy()), opts)
			return e2 == nil, nil
		}
		glog.Errorf("Attempt %d failed to update PgBouncer %s/%s due to %v.", attempt, cur.Namespace, cur.Name, e2)
		return false, nil
	})

	if err != nil {
		err = fmt.Errorf("failed to update PgBouncer %s/%s after %d attempts due to %v", meta.Namespace, meta.Name, attempt, err)
	}
	return
}

func UpdatePgBouncerStatus(
	ctx context.Context,
	c cs.KubedbV1alpha2Interface,
	meta metav1.ObjectMeta,
	transform func(*api.PgBouncerStatus) *api.PgBouncerStatus,
	opts metav1.UpdateOptions,
) (result *api.PgBouncer, err error) {
	apply := func(x *api.PgBouncer) *api.PgBouncer {
		return &api.PgBouncer{
			TypeMeta:   x.TypeMeta,
			ObjectMeta: x.ObjectMeta,
			Spec:       x.Spec,
			Status:     *transform(x.Status.DeepCopy()),
		}
	}

	attempt := 0
	cur, err := c.PgBouncers(meta.Namespace).Get(ctx, meta.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	err = wait.PollImmediate(kutil.RetryInterval, kutil.RetryTimeout, func() (bool, error) {
		attempt++
		var e2 error
		result, e2 = c.PgBouncers(meta.Namespace).UpdateStatus(ctx, apply(cur), opts)
		if kerr.IsConflict(e2) {
			latest, e3 := c.PgBouncers(meta.Namespace).Get(ctx, meta.Name, metav1.GetOptions{})
			switch {
			case e3 == nil:
				cur = latest
				return false, nil
			case kutil.IsRequestRetryable(e3):
				return false, nil
			default:
				return false, e3
			}
		} else if err != nil && !kutil.IsRequestRetryable(e2) {
			return false, e2
		}
		return e2 == nil, nil
	})

	if err != nil {
		err = fmt.Errorf("failed to update status of PgBouncer %s/%s after %d attempts due to %v", meta.Namespace, meta.Name, attempt, err)
	}
	return
}