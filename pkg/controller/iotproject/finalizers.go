/*
 * Copyright 2019, EnMasse authors.
 * License: Apache License 2.0 (see the file LICENSE or http://apache.org/licenses/LICENSE-2.0.html).
 */

package iotproject

import (
	"context"
	"fmt"
	"time"

	"github.com/enmasseproject/enmasse/pkg/util"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/enmasseproject/enmasse/pkg/util/install"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/enmasseproject/enmasse/pkg/util/recon"

	"github.com/enmasseproject/enmasse/pkg/apis/enmasse/v1beta1"
	userv1beta1 "github.com/enmasseproject/enmasse/pkg/apis/user/v1beta1"

	iotv1alpha1 "github.com/enmasseproject/enmasse/pkg/apis/iot/v1alpha1"
	"github.com/enmasseproject/enmasse/pkg/util/finalizer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var finalizers = []finalizer.Finalizer{
	{
		Name:        "iot.enmasse.io/resources",
		Deconstruct: deconstructResources,
	},
	{
		Name:        "iot.enmasse.io/deviceRegistryCleanup",
		Deconstruct: cleanupDeviceRegistry,
	},
}

func deconstructResources(ctx finalizer.DeconstructorContext) (reconcile.Result, error) {

	project, ok := ctx.Object.(*iotv1alpha1.IoTProject)

	if !ok {
		return reconcile.Result{}, fmt.Errorf("provided wrong object type to finalizer, only supports IoTProject")
	}

	if project.Spec.DownstreamStrategy.ManagedDownstreamStrategy != nil {
		return deconstructManagedResources(project, ctx)
	}

	// nothing to do

	return reconcile.Result{}, nil
}

func deconstructManagedResources(project *iotv1alpha1.IoTProject, ctx finalizer.DeconstructorContext) (reconcile.Result, error) {

	rc := recon.ReconcileContext{}

	if project.Status.Managed != nil {
		rc.Process(func() (result reconcile.Result, e error) {
			return cleanupManagedResources(ctx.Context, ctx.Client, project)
		})
	}

	return rc.Result()

}

// delete all managed resources by the project
func cleanupManagedResources(ctx context.Context, c client.Client, project *iotv1alpha1.IoTProject) (reconcile.Result, error) {

	rc := recon.ReconcileContext{}

	managed := project.Status.Managed

	if managed == nil {
		return rc.Result()
	}

	for _, a := range Addresses {
		rc.Process(func() (result reconcile.Result, e error) {

			addressName := util.AddressName(project, a)
			addressMetaName := util.EncodeAddressSpaceAsMetaName(managed.AddressSpace, addressName)

			return cleanupResource(ctx, c, project, client.ObjectKey{
				Namespace: project.Namespace,
				Name:      addressMetaName,
			}, &v1beta1.Address{})

		})
	}

	rc.Process(func() (result reconcile.Result, e error) {
		return cleanupResource(ctx, c, project, client.ObjectKey{
			Namespace: project.Namespace,
			Name:      managed.AddressSpace,
		}, &v1beta1.AddressSpace{})
	})

	rc.Process(func() (result reconcile.Result, e error) {
		return cleanupResource(ctx, c, project, client.ObjectKey{
			Namespace: project.Namespace,
			Name:      managed.AddressSpace + "." + string(project.UID),
		}, &userv1beta1.MessagingUser{})
	})

	return rc.Result()

}

func cleanupResource(ctx context.Context, c client.Client, project *iotv1alpha1.IoTProject, key client.ObjectKey, obj runtime.Object) (reconcile.Result, error) {

	err := c.Get(ctx, key, obj)

	if err != nil {

		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		} else {
			return reconcile.Result{}, err
		}

	}

	result, err := install.RemoveAsOwner(project, obj, false)

	if err != nil {
		return reconcile.Result{}, err
	}

	switch result {
	case install.Found:
		err = c.Update(ctx, obj)
	case install.FoundAndEmpty:
		err = c.Delete(ctx, obj)
	}

	return reconcile.Result{}, err

}

func cleanupDeviceRegistry(ctx finalizer.DeconstructorContext) (reconcile.Result, error) {

	project, ok := ctx.Object.(*iotv1alpha1.IoTProject)

	if !ok {
		return reconcile.Result{}, fmt.Errorf("provided wrong object type to finalizer, only supports IoTProject")
	}

	config, err := getIoTConfigInstance(ctx.Context, ctx.Reader)
	if err != nil {
		return reconcile.Result{}, err
	}

	// check for device registry type

	if config.Spec.ServicesConfig.DeviceRegistry.Infinispan == nil {
		// not required ... complete
		return reconcile.Result{}, nil
	}

	// process

	job, err := createIoTTenantCleanerJob(ctx, project, config)

	// failed to create job

	if err != nil {
		return reconcile.Result{}, err
	}

	// eval job status

	if isComplete(job) {
		// done
		err := deleteJob(ctx, job)
		// remove finalizer
		return reconcile.Result{}, err
	} else if isFailed(job) {
		// restart
		err := deleteJob(ctx, job)
		// keep finalizer
		log.Info("Re-queue: tenant cleanup job failed")
		return reconcile.Result{Requeue: true}, err
	} else {
		// wait
		return reconcile.Result{RequeueAfter: time.Minute}, nil
	}

}

func deleteJob(ctx finalizer.DeconstructorContext, job *batchv1.Job) error {
	p := metav1.DeletePropagationBackground
	return install.DeleteIgnoreNotFound(ctx.Context, ctx.Client, job, &client.DeleteOptions{
		PropagationPolicy: &p,
	})
}
