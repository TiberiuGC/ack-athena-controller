// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package util

import (
	"context"

	ackv1alpha1 "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"
	ackcompare "github.com/aws-controllers-k8s/runtime/pkg/compare"
	"github.com/aws-controllers-k8s/runtime/pkg/metrics"
	ackrtlog "github.com/aws-controllers-k8s/runtime/pkg/runtime/log"
	acktags "github.com/aws-controllers-k8s/runtime/pkg/tags"
	svcsdk "github.com/aws/aws-sdk-go/service/athena"
	"github.com/aws/aws-sdk-go/service/athena/athenaiface"

	svcapitypes "github.com/aws-controllers-k8s/athena-controller/apis/v1alpha1"
)

// var requeueWaitWhileTagUpdated = ackrequeue.NeededAfter(
// 	errors.New("tags Update is in progress"),
// 	time.Second,
// )

// GetTags retrieves the resource's associated tags.
func GetTags(
	ctx context.Context,
	sdkapi athenaiface.AthenaAPI,
	metrics *metrics.Metrics,
	resourceARN string,
) ([]*svcapitypes.Tag, error) {
	resp, err := sdkapi.ListTagsForResourceWithContext(
		ctx,
		&svcsdk.ListTagsForResourceInput{
			ResourceARN: &resourceARN,
		},
	)
	metrics.RecordAPICall("GET", "ListTagsForResource", err)
	if err != nil {
		return nil, err
	}
	tags := make([]*svcapitypes.Tag, 0, len(resp.Tags))
	for _, tag := range resp.Tags {
		tags = append(tags, &svcapitypes.Tag{
			Key:   tag.Key,
			Value: tag.Value,
		})
	}
	return tags, nil
}

// SyncTags examines the Tags in the supplied Resource and calls the
// TagResourceWithContext and UntagResourceWithContext APIs to ensure that the set of
// associated Tags stays in sync with the Resource.Spec.Tags
func SyncTags(
	ctx context.Context,
	desiredTags []*svcapitypes.Tag,
	latestTags []*svcapitypes.Tag,
	latestACKResourceMetadata *ackv1alpha1.ResourceMetadata,
	toACKTags func(tags []*svcapitypes.Tag) acktags.Tags,
	sdkapi athenaiface.AthenaAPI,
	metrics *metrics.Metrics,
) (err error) {
	rlog := ackrtlog.FromContext(ctx)
	exit := rlog.Trace("rm.syncTags")
	defer func() { exit(err) }()

	arn := (*string)(latestACKResourceMetadata.ARN)

	from := toACKTags(latestTags)
	to := toACKTags(desiredTags)

	added, _, removed := ackcompare.GetTagsDifference(from, to)

	for key := range removed {
		if _, ok := added[key]; ok {
			delete(removed, key)
		}
	}

	if len(added) > 0 {
		toAdd := make([]*svcsdk.Tag, 0, len(added))
		for key, val := range added {
			key, val := key, val
			toAdd = append(toAdd, &svcsdk.Tag{
				Key:   &key,
				Value: &val,
			})
		}
		rlog.Debug("adding tags to work group", "tags", added)
		_, err = sdkapi.TagResourceWithContext(
			ctx,
			&svcsdk.TagResourceInput{
				ResourceARN: arn,
				Tags:        toAdd,
			},
		)
		metrics.RecordAPICall("UPDATE", "AddTagsToResource", err)
		if err != nil {
			return err
		}
	}

	if len(removed) > 0 {
		toRemove := make([]*string, 0, len(removed))
		for key := range removed {
			key := key
			toRemove = append(toRemove, &key)
		}
		rlog.Debug("removing tags from work group", "tags", removed)
		_, err = sdkapi.UntagResourceWithContext(
			ctx,
			&svcsdk.UntagResourceInput{
				ResourceARN: arn,
				TagKeys:     toRemove,
			},
		)
		metrics.RecordAPICall("UPDATE", "RemoveTagsFromResource", err)
		if err != nil {
			return err
		}
	}

	return nil
}
