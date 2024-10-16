// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2021 Renesas Electronics Corporation.
// Copyright (C) 2021 EPAM Systems, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmserver_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/aoscloud/aos_common/api/cloudprotocol"
	pb "github.com/aoscloud/aos_common/api/communicationmanager/v2"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/aoscloud/aos_communicationmanager/cmserver"
	"github.com/aoscloud/aos_communicationmanager/config"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const (
	serverURL = "localhost:8094"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

type testClient struct {
	connection *grpc.ClientConn
	pbclient   pb.UpdateSchedulerServiceClient
}

type testUpdateHandler struct {
	fotaChannel chan cmserver.UpdateFOTAStatus
	sotaChannel chan cmserver.UpdateSOTAStatus
	startFOTA   bool
	startSOTA   bool
}

/***********************************************************************************************************************
 * Init
 **********************************************************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true,
	})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/***********************************************************************************************************************
 * Tests
 **********************************************************************************************************************/

func TestConnection(t *testing.T) {
	cmConfig := config.Config{
		CMServerURL: serverURL,
	}

	unitStatusHandler := testUpdateHandler{
		sotaChannel: make(chan cmserver.UpdateSOTAStatus, 10),
		fotaChannel: make(chan cmserver.UpdateFOTAStatus, 10),
	}

	cmServer, err := cmserver.New(&cmConfig, &unitStatusHandler, nil, nil, true)
	if err != nil {
		t.Fatalf("Can't create CM server: %s", err)
	}
	defer cmServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}

	stream, err := client.pbclient.SubscribeNotifications(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("Can't subscribe: %s", err)
	}

	for i := 0; i < 2; i++ {
		notification, err := stream.Recv()
		if err != nil {
			t.Fatalf("Can't receive notification: %s", err)
		}

		switch notification.SchedulerNotification.(type) {
		case *pb.SchedulerNotifications_FotaStatus:
			if notification.GetFotaStatus().State != pb.UpdateState_NO_UPDATE {
				t.Error("Incorrect state: ", notification.GetFotaStatus().State.String())
			}

		case *pb.SchedulerNotifications_SotaStatus:
			if notification.GetSotaStatus().State != pb.UpdateState_NO_UPDATE {
				t.Error("Incorrect state: ", notification.GetSotaStatus().State.String())
			}
		}
	}

	statusFotaNotification := cmserver.UpdateFOTAStatus{
		Components:   []cloudprotocol.ComponentStatus{{ID: "1234", AosVersion: 123, VendorVersion: "4321"}},
		UnitConfig:   &cloudprotocol.UnitConfigStatus{VendorVersion: "bc_version"},
		UpdateStatus: cmserver.UpdateStatus{State: cmserver.ReadyToUpdate},
	}

	unitStatusHandler.fotaChannel <- statusFotaNotification

	notification, err := stream.Recv()
	if err != nil {
		t.Fatalf("Can't receive notification: %s", err)
	}

	status := notification.GetFotaStatus()
	if status == nil {
		t.Fatalf("No FOTA status")
	}

	if status.State != pb.UpdateState_READY_TO_UPDATE {
		t.Error("Incorrect state: ", status.State.String())
	}

	if len(status.Components) != 1 {
		t.Fatal("Incorrect count of components")
	}

	if status.Components[0].Id != "1234" {
		t.Error("Incorrect component id")
	}

	if status.Components[0].VendorVersion != "4321" {
		t.Error("Incorrect vendor version")
	}

	if status.Components[0].AosVersion != 123 {
		t.Error("Incorrect aos version")
	}

	if status.UnitConfig == nil {
		t.Fatal("Unit Config is nil")
	}

	if status.UnitConfig.VendorVersion != "bc_version" {
		t.Error("Incorrect unit config version")
	}

	statusNotification := cmserver.UpdateSOTAStatus{
		InstallServices: []cloudprotocol.ServiceStatus{{ID: "s1", AosVersion: 42}},
		RemoveServices:  []cloudprotocol.ServiceStatus{{ID: "s2", AosVersion: 42}},
		InstallLayers:   []cloudprotocol.LayerStatus{{ID: "l1", Digest: "someSha", AosVersion: 42}},
		RemoveLayers:    []cloudprotocol.LayerStatus{{ID: "l2", Digest: "someSha", AosVersion: 42}},
		UpdateStatus:    cmserver.UpdateStatus{State: cmserver.Downloading, Error: "SOTA error"},
	}

	unitStatusHandler.sotaChannel <- statusNotification

	notification, err = stream.Recv()
	if err != nil {
		t.Fatalf("Can't receive notification: %s", err)
	}

	sotaStatus := notification.GetSotaStatus()
	if sotaStatus == nil {
		t.Fatalf("No SOTA status")
	}

	if sotaStatus.State != pb.UpdateState_DOWNLOADING {
		t.Error("Incorrect state: ", status.State.String())
	}

	if sotaStatus.Error != "SOTA error" {
		t.Error("Incorrect error message: ", status.Error)
	}

	if len(sotaStatus.InstallServices) != 1 {
		t.Fatal("Incorrect count of services")
	}

	if sotaStatus.InstallServices[0].Id != "s1" {
		t.Error("Incorrect service id")
	}

	if sotaStatus.InstallServices[0].AosVersion != 42 {
		t.Error("Incorrect service aos version")
	}

	if len(sotaStatus.InstallLayers) != 1 {
		t.Fatal("Incorrect count of layers")
	}

	if sotaStatus.InstallLayers[0].Id != "l1" {
		t.Error("Incorrect layer id")
	}

	if sotaStatus.InstallLayers[0].Digest != "someSha" {
		t.Error("Incorrect layer digest")
	}

	if sotaStatus.InstallLayers[0].AosVersion != 42 {
		t.Error("Incorrect layer aos version")
	}

	if _, err := client.pbclient.StartFOTAUpdate(ctx, &emptypb.Empty{}); err != nil {
		t.Fatalf("Can't start FOTA update: %v", err)
	}

	if !unitStatusHandler.startFOTA {
		t.Error("FOTA update should be started")
	}

	if _, err := client.pbclient.StartSOTAUpdate(ctx, &emptypb.Empty{}); err != nil {
		t.Fatalf("Can't start SOTA update: %v", err)
	}

	if !unitStatusHandler.startSOTA {
		t.Error("SOTA update should be started")
	}

	client.close()

	time.Sleep(time.Second)
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func newTestClient(url string) (client *testClient, err error) {
	client = &testClient{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if client.connection, err = grpc.DialContext(
		ctx, url, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock()); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	client.pbclient = pb.NewUpdateSchedulerServiceClient(client.connection)

	return client, nil
}

func (client *testClient) close() {
	if client.connection != nil {
		client.connection.Close()
	}
}

func (handler *testUpdateHandler) GetFOTAStatusChannel() (channel <-chan cmserver.UpdateFOTAStatus) {
	return handler.fotaChannel
}

func (handler *testUpdateHandler) GetSOTAStatusChannel() (channel <-chan cmserver.UpdateSOTAStatus) {
	return handler.sotaChannel
}

func (handler *testUpdateHandler) GetFOTAStatus() (status cmserver.UpdateFOTAStatus) {
	status.State = cmserver.NoUpdate

	return status
}

func (handler *testUpdateHandler) GetSOTAStatus() (status cmserver.UpdateSOTAStatus) {
	status.State = cmserver.NoUpdate

	return status
}

func (handler *testUpdateHandler) StartFOTAUpdate() (err error) {
	handler.startFOTA = true

	return nil
}

func (handler *testUpdateHandler) StartSOTAUpdate() (err error) {
	handler.startSOTA = true

	return nil
}
