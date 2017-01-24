package awsecs

import (
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/weaveworks/scope/probe/docker"
	"github.com/weaveworks/scope/report"
)

// TaskFamily is the key that stores the task family of an ECS Task
const (
	Cluster             = "ecs_cluster"
	CreatedAt           = "ecs_created_at"
	TaskFamily          = "ecs_task_family"
	ServiceDesiredCount = "ecs_service_desired_count"
	ServiceRunningCount = "ecs_service_running_count"
)

var (
	taskMetadata = report.MetadataTemplates{
		Cluster:    {ID: Cluster, Label: "Cluster", From: report.FromLatest, Priority: 0},
		CreatedAt:  {ID: CreatedAt, Label: "Created At", From: report.FromLatest, Priority: 1, Datatype: "datetime"},
		TaskFamily: {ID: TaskFamily, Label: "Family", From: report.FromLatest, Priority: 2},
	}
	serviceMetadata = report.MetadataTemplates{
		Cluster:             {ID: Cluster, Label: "Cluster", From: report.FromLatest, Priority: 0},
		CreatedAt:           {ID: CreatedAt, Label: "Created At", From: report.FromLatest, Priority: 1, Datatype: "datetime"},
		ServiceDesiredCount: {ID: ServiceDesiredCount, Label: "Desired Tasks", From: report.FromLatest, Priority: 2, Datatype: "number"},
		ServiceRunningCount: {ID: ServiceRunningCount, Label: "Running Tasks", From: report.FromLatest, Priority: 3, Datatype: "number"},
	}
)

// TaskLabelInfo is used in return value of GetLabelInfo. Exported for test.
type TaskLabelInfo struct {
	ContainerIDs []string
	Family       string
}

// GetLabelInfo returns map from cluster to map of task arns to task infos.
// Exported for test.
func GetLabelInfo(rpt report.Report) map[string]map[string]*TaskLabelInfo {
	results := map[string]map[string]*TaskLabelInfo{}
	log.Debug("scanning for ECS containers")
	for nodeID, node := range rpt.Container.Nodes {

		taskArn, ok := node.Latest.Lookup(docker.LabelPrefix + "com.amazonaws.ecs.task-arn")
		if !ok {
			continue
		}

		cluster, ok := node.Latest.Lookup(docker.LabelPrefix + "com.amazonaws.ecs.cluster")
		if !ok {
			continue
		}

		family, ok := node.Latest.Lookup(docker.LabelPrefix + "com.amazonaws.ecs.task-definition-family")
		if !ok {
			continue
		}

		taskMap, ok := results[cluster]
		if !ok {
			taskMap = map[string]*TaskLabelInfo{}
			results[cluster] = taskMap
		}

		task, ok := taskMap[taskArn]
		if !ok {
			task = &TaskLabelInfo{ContainerIDs: []string{}, Family: family}
			taskMap[taskArn] = task
		}

		task.ContainerIDs = append(task.ContainerIDs, nodeID)
	}
	log.Debug("Got ECS container info: %v", results)
	return results
}

// Reporter implements Tagger, Reporter
type Reporter struct {
	ClientsByCluster map[string]EcsClient // Exported for test
	cacheSize        int
	cacheExpiry      time.Duration
}

// Make creates a new Reporter
func Make(cacheSize int, cacheExpiry time.Duration) Reporter {
	return Reporter{
		ClientsByCluster: map[string]EcsClient{},
		cacheSize:        cacheSize,
		cacheExpiry:      cacheExpiry,
	}
}

// Tag needed for Tagger
func (r Reporter) Tag(rpt report.Report) (report.Report, error) {
	rpt = rpt.Copy()

	clusterMap := GetLabelInfo(rpt)

	for cluster, taskMap := range clusterMap {
		log.Debugf("Fetching ECS info for cluster %v with %v tasks", cluster, len(taskMap))

		client, ok := r.ClientsByCluster[cluster]
		if !ok {
			log.Debugf("Creating new ECS client")
			var err error
			client, err = newClient(cluster, r.cacheSize, r.cacheExpiry)
			if err != nil {
				return rpt, err
			}
			r.ClientsByCluster[cluster] = client
		}

		taskArns := make([]string, 0, len(taskMap))
		for taskArn := range taskMap {
			taskArns = append(taskArns, taskArn)
		}

		ecsInfo := client.GetInfo(taskArns)
		log.Debugf("Got info from ECS: %d tasks, %d services", len(ecsInfo.Tasks), len(ecsInfo.Services))

		// Create all the services first
		for serviceName, service := range ecsInfo.Services {
			serviceID := report.MakeECSServiceNodeID(serviceName)
			rpt.ECSService = rpt.ECSService.AddNode(report.MakeNodeWith(serviceID, map[string]string{
				Cluster:             cluster,
				ServiceDesiredCount: fmt.Sprintf("%d", service.DesiredCount),
				ServiceRunningCount: fmt.Sprintf("%d", service.RunningCount),
			}))
		}
		log.Debugf("Created %v ECS service nodes", len(ecsInfo.Services))

		for taskArn, info := range taskMap {
			task, ok := ecsInfo.Tasks[taskArn]
			if !ok {
				// can happen due to partial failures, just skip it
				continue
			}

			// new task node
			taskID := report.MakeECSTaskNodeID(taskArn)
			node := report.MakeNodeWith(taskID, map[string]string{
				TaskFamily: info.Family,
				Cluster:    cluster,
				CreatedAt:  task.CreatedAt.Format(time.RFC3339Nano),
			})
			rpt.ECSTask = rpt.ECSTask.AddNode(node)

			// parents sets to merge into all matching container nodes
			parentsSets := report.MakeSets()
			parentsSets = parentsSets.Add(report.ECSTask, report.MakeStringSet(taskID))
			if serviceName, ok := ecsInfo.TaskServiceMap[taskArn]; ok {
				serviceID := report.MakeECSServiceNodeID(serviceName)
				parentsSets = parentsSets.Add(report.ECSService, report.MakeStringSet(serviceID))
			}
			for _, containerID := range info.ContainerIDs {
				if containerNode, ok := rpt.Container.Nodes[containerID]; ok {
					rpt.Container.Nodes[containerID] = containerNode.WithParents(parentsSets)
				} else {
					log.Warnf("Got task info for non-existent container %v, this shouldn't be able to happen", containerID)
				}
			}
		}

	}

	return rpt, nil
}

// Report needed for Reporter
func (Reporter) Report() (report.Report, error) {
	result := report.MakeReport()
	taskTopology := report.MakeTopology().WithMetadataTemplates(taskMetadata)
	result.ECSTask = result.ECSTask.Merge(taskTopology)
	serviceTopology := report.MakeTopology().WithMetadataTemplates(serviceMetadata)
	result.ECSService = result.ECSService.Merge(serviceTopology)
	return result, nil
}

// Name needed for Tagger, Reporter
func (r Reporter) Name() string {
	return "awsecs"
}
