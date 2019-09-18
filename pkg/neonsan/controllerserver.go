/*
Copyright 2018 Yunify, Inc.

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

package neonsan

import (
	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/golang/glog"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"github.com/yunify/qingstor-csi/pkg/neonsan/manager"
	"github.com/yunify/qingstor-csi/pkg/neonsan/util"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type controllerServer struct {
	*csicommon.DefaultControllerServer
	creatingVolume map[string]bool
}

// This operation MUST be idempotent
// csi.CreateVolumeRequest: name 				+Required
//							capability			+Required
func (cs *controllerServer) CreateVolume(ctx context.Context,
	req *csi.CreateVolumeRequest) (resp *csi.CreateVolumeResponse, err error) {
	defer util.EntryFunction("CreateVolume")()

	glog.Info("Validate input arguments.")
	// Valid controller service capability
	if err := cs.Driver.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		glog.Errorf("Invalid create volume req: %v.", req)
		return nil, err
	}

	// Required volume capability
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities missing in request.")
	} else if !util.ContainsVolumeCapabilities(cs.Driver.GetVolumeCapabilityAccessModes(), req.GetVolumeCapabilities()) {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities not match.")
	}

	// Required volume name
	if len(req.GetName()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume name missing in request.")
	}
	volumeName := req.GetName()

	// Ensure no more than one call "in-flight" per volume
	if _, found := cs.creatingVolume[volumeName]; found {
		return nil, status.Errorf(codes.Aborted, "no more than one call in-flight for volume [%v]", req)
	} else {
		cs.creatingVolume[volumeName] = true
		defer delete(cs.creatingVolume, volumeName)
	}

	// Create StorageClass object
	glog.Info("Create StorageClass object.")
	sc, err := manager.NewNeonsanStorageClassFromMap(req.GetParameters())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Get request volume size range
	glog.Info("Get request volume size.")
	requiredByte := req.GetCapacityRange().GetRequiredBytes()
	limitByte := req.GetCapacityRange().GetLimitBytes()
	requiredFormatByte := util.FormatVolumeSize(requiredByte, util.Gib*int64(sc.StepSize))
	if limitByte == 0 {
		limitByte = util.Int64Max
	}

	// Check volume range
	if requiredFormatByte > limitByte {
		glog.Errorf("Request capacity range [%d, %d] bytes, format required size: [%d] gb.",
			requiredByte, limitByte, requiredFormatByte)
		return nil, status.Error(codes.OutOfRange, "Unsupported capacity range.")
	}

	// For idempotent:
	// Find exist volume name
	glog.Infof("Find duplicate volume name [%s].", volumeName)
	exVol, err := manager.FindVolume(volumeName, sc.Pool)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if exVol != nil {
		glog.Infof("Request volume name: [%s], size: [%d], capacity range [%d,%d] Bytes, pool: [%s], replicas: [%d].",
			volumeName, requiredFormatByte, requiredByte, limitByte, sc.Pool, sc.Replicas)
		glog.Infof("Exist volume name: [%s], id: [%s], capacity: [%d] Bytes, pool: [%s], replicas: [%d].",
			exVol.Name, exVol.Id, exVol.SizeByte, exVol.Pool, exVol.Replicas)
		if exVol.SizeByte >= requiredByte && exVol.SizeByte <= limitByte && exVol.Replicas == sc.Replicas {
			// exisiting volume is compatible with new request and should be
			// reused.
			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					Id:            exVol.Name,
					CapacityBytes: exVol.SizeByte,
					Attributes:    req.GetParameters(),
				},
			}, nil
		}
		return nil, status.Errorf(codes.AlreadyExists, "Volume [%s] already exists but is incompatible.", volumeName)
	}
	glog.Infof("Not Found duplicate volume name [%s].", volumeName)

	// Do create volume
	glog.Infof("Creating volume [%s] with [%d] bytes in pool [%s]...", volumeName, requiredFormatByte, sc.Pool)
	volumeInfo, err := manager.CreateVolume(volumeName, sc.Pool, requiredFormatByte, sc.Replicas)
	if err != nil {
		glog.Errorf("Failed to create volume [%s] with [%d] bytes in pool [%s] with error [%v].", volumeName,
			requiredFormatByte, sc.Pool, err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	glog.Infof("Succeed to create volume [%s] with [%d] bytes in pool [%s].", volumeName, requiredFormatByte,
		sc.Pool)

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			Id:            volumeInfo.Name,
			CapacityBytes: volumeInfo.SizeByte,
			Attributes:    req.GetParameters(),
		},
	}, nil
}

// This operation MUST be idempotent
// volume id is REQUIRED in csi.DeleteVolumeRequest
func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	defer util.EntryFunction("DeleteVolume")()

	glog.Info("Validate input arguments.")
	// Valid controller service capability
	if err := cs.Driver.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		glog.Errorf("Invalid delete volume req: %v.", req)
		return nil, err
	}

	// Check sanity of request Name, Volume Capabilities
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume id missing in request.")
	}
	// For now the image get unconditionally deleted, but here retention
	// policy can be checked
	volumeId := req.GetVolumeId()

	// For idempotent:
	// MUST reply OK when volume does not exist
	glog.Infof("Find volume [%s].", volumeId)
	volInfo, err := manager.FindVolumeWithoutPool(volumeId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if volInfo == nil {
		glog.Warningf("Not found volume [%s].", volumeId)
		return &csi.DeleteVolumeResponse{}, nil
	}
	glog.Infof("Found volume [%s].", volumeId)

	// Do delete volume
	glog.Infof("Deleting volume [%s] in pool [%s]...", volumeId, volInfo.Pool)
	err = manager.DeleteVolume(volumeId, volInfo.Pool)
	if err != nil {
		glog.Errorf("Failed to delete volume: [%s] in pool [%s] with error: [%v].", volumeId, volInfo.Pool, err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	glog.Infof("Succeed to delete volume: [%s] in pool [%s]", volumeId, volInfo.Pool)
	return &csi.DeleteVolumeResponse{}, nil
}

// This operation MUST be idempotent
// csi.ValidateVolumeCapabilitiesRequest: 	volume id 			+ Required
// 											volume capability 	+ Required
func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	defer util.EntryFunction("ValidateVolumeCapabilities")()

	glog.Info("Validate input arguments.")
	// require volume id parameter
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "No volume id is provided")
	}

	// require capability parameter
	if len(req.GetVolumeCapabilities()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "No volume capabilities are provided")
	}

	// Create StorageClass object
	glog.Info("Create StorageClass object.")
	sc, err := manager.NewNeonsanStorageClassFromMap(req.GetVolumeAttributes())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// check volume exist
	volumeId := req.GetVolumeId()
	glog.Infof("Find volume [%s] in pool [%s].", volumeId, sc.Pool)
	outVol, err := manager.FindVolume(volumeId, sc.Pool)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if outVol == nil {
		glog.Errorf("Not found volume [%s] in pool [%s].", volumeId, sc.Pool)
		return nil, status.Errorf(codes.NotFound, "Volume %s does not exist.", volumeId)
	}
	glog.Infof("Found volume [%s] in pool [%s].", volumeId, sc.Pool)

	// check capability
	glog.Info("Check capability.")
	for _, c := range req.GetVolumeCapabilities() {
		found := false
		for _, c1 := range cs.Driver.GetVolumeCapabilityAccessModes() {
			if c1.GetMode() == c.GetAccessMode().GetMode() {
				found = true
			}
		}
		if !found {
			return &csi.ValidateVolumeCapabilitiesResponse{
				Supported: false,
				Message:   "Driver does not support mode:" + c.GetAccessMode().GetMode().String(),
			}, nil
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Supported: true,
	}, nil
}

// GetCapacity: allow the CO to query the capacity of the storage pool from
// which the controller provisions volumes.
func (cs *controllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	defer util.EntryFunction("GetCapacity")()

	// Create StorageClass object
	glog.Info("Create StorageClass object.")
	sc, err := manager.NewNeonsanStorageClassFromMap(req.GetParameters())
	if err != nil {
		glog.Info("Failed to create StorageClass object.")
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	glog.Info("Succeed to create StorageClass object.")

	// Find pool information
	poolInfo, err := manager.FindPool(sc.Pool)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if poolInfo == nil {
		glog.Infof("Cannot find pool [%s].", sc.Pool)
		return nil, status.Error(codes.NotFound, err.Error())
	}

	glog.Infof("Succeed to find pool name [%s], id [%s], total [%d] bytes, free [%d] bytes, used [%d] bytes.",
		poolInfo.Name, poolInfo.Id, poolInfo.TotalByte, poolInfo.FreeByte, poolInfo.UsedByte)
	return &csi.GetCapacityResponse{
		AvailableCapacity: poolInfo.FreeByte,
	}, nil
}
