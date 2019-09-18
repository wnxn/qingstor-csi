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

package manager_test

import (
	"errors"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/yunify/qingstor-csi/pkg/neonsan/manager"
	"github.com/yunify/qingstor-csi/pkg/neonsan/util"
	"reflect"
)

var _ = Describe("Parse Text", func() {
	DescribeTable("parse volume list",
		func(text string, volInfo []*manager.VolumeInfo, err error) {
			resInfo, resErr := manager.ParseVolumeList(text)
			Expect(resErr == nil).To(Equal(err == nil))
			Expect(reflect.DeepEqual(resInfo, volInfo)).To(Equal(true))
		},
		Entry("one volume list",
			`Volume Count:  1
+----------+------+------------+------+---------+-----------+---------------+-------+---------+--------+------------------+-----------+---------------------+---------------------+
|    ID    | NAME |    SIZE    | POOL | RG NAME | REP COUNT | MIN REP COUNT | ROLE  | POLICY  | STATUS | VOLUME ALLOCATED | ENCRYPTED |     STATUS TIME     |    CREATED TIME     |
+----------+------+------------+------+---------+-----------+---------------+-------+---------+--------+------------------+-----------+---------------------+---------------------+
| 33554432 | test | 1073741824 | kube | default |         2 |             1 | alone | default | OK     |                0 | no        | 2019-09-17 15:17:50 | 2019-09-17 15:17:50 |
+----------+------+------------+------+---------+-----------+---------------+-------+---------+--------+------------------+-----------+---------------------+---------------------+`,
			[]*manager.VolumeInfo{
				{
					Id:       "33554432",
					Name:     "test",
					SizeByte: 1073741824,
					Status:   manager.VolumeStatusOk,
					Replicas: 2,
					Pool:     "kube",
				},
			},
			nil),
		Entry("two volumes list",
			`Volume Count:  2
+----------+-------+-------------+------+---------+-----------+---------------+-------+---------+--------+------------------+-----------+---------------------+---------------------+
|    ID    | NAME  |    SIZE     | POOL | RG NAME | REP COUNT | MIN REP COUNT | ROLE  | POLICY  | STATUS | VOLUME ALLOCATED | ENCRYPTED |     STATUS TIME     |    CREATED TIME     |
+----------+-------+-------------+------+---------+-----------+---------------+-------+---------+--------+------------------+-----------+---------------------+---------------------+
| 50331648 | test2 | 10737418240 | kube | default |         2 |             1 | alone | default | OK     |                0 | no        | 2019-09-17 15:19:49 | 2019-09-17 15:19:49 |
| 33554432 | test  |  1073741824 | kube | default |         2 |             1 | alone | default | OK     |                0 | no        | 2019-09-17 15:17:50 | 2019-09-17 15:17:50 |
+----------+-------+-------------+------+---------+-----------+---------------+-------+---------+--------+------------------+-----------+---------------------+---------------------+
`,
			[]*manager.VolumeInfo{
				{
					Id:       "50331648",
					Name:     "test2",
					SizeByte: 10737418240,
					Status:   manager.VolumeStatusOk,
					Replicas: 2,
					Pool:     "kube",
				},
				{
					Id:       "33554432",
					Name:     "test",
					SizeByte: 1073741824,
					Status:   manager.VolumeStatusOk,
					Replicas: 2,
					Pool:     "kube",
				},
			},
			nil),
		Entry("no volume list",
			`Volume Count:0
`,
			nil,
			nil),
	)

	DescribeTable("parse pool info",
		func(text string, info *manager.PoolInfo, err error) {
			resInfo, resErr := manager.ParsePoolInfo(text)
			Expect(resErr == nil).To(Equal(err == nil))
			Expect(resInfo).To(Equal(info))
		},
		Entry("find csi pool",
			`+----------+-----------+-------+------+------+
| POOL ID  | POOL NAME | TOTAL | FREE | USED |
+----------+-----------+-------+------+------+
| 67108864 | csi       |  2982 | 1222 | 1759 |
+----------+-----------+-------+------+------+

`,
			&manager.PoolInfo{
				Id:        "67108864",
				Name:      "csi",
				TotalByte: 2982 * util.Gib,
				FreeByte:  1222 * util.Gib,
				UsedByte:  1759 * util.Gib,
			},
			nil),
		Entry("pool not found",
			`Pool Count:  0`,
			nil,
			nil),
	)

	DescribeTable("parse pool name list",
		func(text string, pools []string, err error) {
			resPools, resErr := manager.ParsePoolNameList(text)
			Expect(resErr == nil).To(Equal(err == nil))
			Expect(resPools).To(Equal(pools))
		},
		Entry("find csi pool",
			`Pool Count:  2
+------+
| NAME |
+------+
| pool |
| kube |
+------+
`,
			[]string{
				"pool",
				"kube",
			},
			nil),
		Entry("pool not found",
			`Pool Count:  0`,
			nil,
			nil),
		Entry("wrong output",
			`wrong output`,
			nil,
			errors.New("wrong output")),
	)

	DescribeTable("parse attached volume list",
		func(text string, info []*manager.AttachInfo) {
			resInfo := manager.ParseAttachVolumeList(text)
			Expect(resInfo).To(Equal(info))
		},
		Entry("two attached volume",
			`dev_id  vol_id  device  volume  config  read_bps    write_bps   read_iops   write_iops
0   0x3ff7000000    qbd0    csi/foo1    /etc/neonsan/qbd.conf   0   0   0   0
1   0x3a7c000000    qbd1    csi/foo /etc/neonsan/qbd.conf   0   0   0   0

`,
			[]*manager.AttachInfo{
				{
					Id:        "274726912000",
					Name:      "foo1",
					Device:    "/dev/qbd0",
					Pool:      "csi",
					ReadBps:   0,
					WriteBps:  0,
					ReadIops:  0,
					WriteIops: 0,
				},
				{
					Id:        "251188477952",
					Name:      "foo",
					Device:    "/dev/qbd1",
					Pool:      "csi",
					ReadBps:   0,
					WriteBps:  0,
					ReadIops:  0,
					WriteIops: 0,
				},
			},
		),
	)

})
