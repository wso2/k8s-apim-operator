// Copyright (c)  WSO2 Inc. (http://www.wso2.org) All Rights Reserved.
//
// WSO2 Inc. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package ingress

import (
	"context"
	"github.com/wso2/k8s-api-operator/api-operator/pkg/apiproject/build"
	gwclient "github.com/wso2/k8s-api-operator/api-operator/pkg/apiproject/client"
	"github.com/wso2/k8s-api-operator/api-operator/pkg/apiproject/names"
	"github.com/wso2/k8s-api-operator/api-operator/pkg/apiproject/status"
	"github.com/wso2/k8s-api-operator/api-operator/pkg/controller/common"
	inghandler "github.com/wso2/k8s-api-operator/api-operator/pkg/ingress/handler"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"path/filepath"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
	"strings"
	"testing"
	"time"
)

func TestReconcile(t *testing.T) {
	ctx := context.Background()
	k8sObjects := make([]runtime.Object, 0, 16)

	// Read ingresses
	ingresses, err := readResources("test_resources/existing/ingresses.yaml", v1beta1.Ingress{})
	if err != nil {
		t.Fatal("Error reading ingress resources")
	}
	k8sObjects = append(k8sObjects, ingresses...)

	// Read status configmap
	statusCm, err := readResources("test_resources/existing/configmaps.yaml", v1.ConfigMap{})
	if err != nil {
		t.Fatal("Error reading configmap resource")
	}
	k8sObjects = append(k8sObjects, statusCm...)

	// Read services
	svc, err := readResources("test_resources/existing/services.yaml", v1.Service{})
	if err != nil {
		t.Fatal("Error reading service resources")
	}
	k8sObjects = append(k8sObjects, svc...)

	// Read secrets
	sec, err := readResources("test_resources/existing/secrets.yaml", v1.Secret{})
	if err != nil {
		t.Fatal("Error reading secret resources")
	}
	k8sObjects = append(k8sObjects, sec...)

	k8sClient := fake.NewFakeClientWithScheme(scheme.Scheme, k8sObjects...)

	r := &ReconcileIngress{
		client:      k8sClient,
		scheme:      scheme.Scheme,
		evnRecorder: &record.FakeRecorder{},
		ingHandler:  &inghandler.Handler{AdapterClient: gwclient.NewFakeAllSucceeded()},
	}
	var request reconcile.Request

	// 1.  Update whole world
	t.Run("Build_whole_world", func(t *testing.T) {
		for _, ingress := range ingresses {
			ing := ingress.(*v1beta1.Ingress)
			request = reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ing.Namespace, Name: ing.Name}}
			if _, err := r.Reconcile(request); err != nil {
				t.Error("Error building whole world from initial ingresses")
			}

			// The following is not a required feature, but it can void unnecessary update of gateway
			if r.ingHandler.AdapterClient.(*gwclient.Fake).ProjectMap != nil {
				t.Error("Only last request should consider to build whole world")
			}
		}
		// Since update ingresses with finalizers will result to requeue the updated ingress
		// process them again
		for i, ingress := range ingresses {
			ing := *ingress.(*v1beta1.Ingress)
			request = reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ing.Namespace, Name: ing.Name}}
			if _, err := r.Reconcile(request); err != nil {
				t.Error("Error building whole world from initial ingresses")
			}

			// The following is not a required feature, but it can provide unnecessary update of gateway
			if i < len(ingresses)-1 && r.ingHandler.AdapterClient.(*gwclient.Fake).ProjectMap != nil {
				t.Error("Only last request should consider to build whole world")
			}
		}

		projectMap := r.ingHandler.AdapterClient.(*gwclient.Fake).ProjectMap
		testAction(t, projectMap, "Ing 1", "ingress-__bar_com", build.ForceUpdate)
		testAction(t, projectMap, "Ing 1", names.NoVHostProject, build.ForceUpdate)
	})

	// 2.  Add new ingress: ing5
	t.Run("Delta_change:_Add_new_ingress", func(t *testing.T) {
		ing, err := readResources("test_resources/new/new-ing5.yaml", v1beta1.Ingress{})
		if err != nil {
			t.Fatal("Error reading ingress resource")
		}
		newIng5 := ing[0].(*v1beta1.Ingress)
		request = reconcile.Request{NamespacedName: types.NamespacedName{Namespace: newIng5.Namespace, Name: newIng5.Name}}
		if err := k8sClient.Create(ctx, newIng5); err != nil {
			t.Fatal("Error in k8s client; err: ", err)
		}
		// Reconcile will update finalizers and requeue request
		// So handle another reconcile
		if _, err := r.Reconcile(request); err != nil {
			t.Error("Error building delta update")
		}
		if _, err := r.Reconcile(request); err != nil {
			t.Error("Error building delta update")
		}

		projectMap := r.ingHandler.AdapterClient.(*gwclient.Fake).ProjectMap
		testAction(t, projectMap, "Ing 5", "ingress-__bar_com", build.ForceUpdate)
		// although the service is no available, ingress should continue
		testAction(t, projectMap, "Ing 5", "ingress-__no-service_com", build.ForceUpdate)

		testCurrentStatus(k8sClient, t, true, "default/ing5", "ingress-__bar_com")
		// although the service is no available, ingress should continue
		testCurrentStatus(k8sClient, t, true, "default/ing5", "ingress-__no-service_com")
	})

	// 3.  Update ingress: ing1
	t.Run("Delta_change:_Update_ingress", func(t *testing.T) {
		ing, err := readResources("test_resources/new/update-ing1.yaml", v1beta1.Ingress{})
		if err != nil {
			t.Fatal("Error reading ingress resource")
		}
		updateIng1 := ing[0].(*v1beta1.Ingress)

		request = reconcile.Request{NamespacedName: types.NamespacedName{Namespace: updateIng1.Namespace, Name: updateIng1.Name}}
		if err := k8sClient.Update(ctx, updateIng1); err != nil {
			t.Fatal("Error in k8s client; err: ", err)
		}
		// Reconcile will update finalizers and requeue request
		// So handle another reconcile
		if _, err := r.Reconcile(request); err != nil {
			t.Error("Error building delta update; err: ", err)
		}
		if _, err := r.Reconcile(request); err != nil {
			t.Error("Error building delta update; err: ", err)
		}

		projectMap := r.ingHandler.AdapterClient.(*gwclient.Fake).ProjectMap
		testAction(t, projectMap, "Ing 1", names.NoVHostProject, build.ForceUpdate)
		testAction(t, projectMap, "Ing 1", "ingress-__foo_com", build.ForceUpdate)
		testAction(t, projectMap, "Ing 1", "ingress-prod_foo_com", build.ForceUpdate)
		testAction(t, projectMap, "Ing 1", "ingress-deprecated_foo_com", build.Delete)
		testAction(t, projectMap, "Ing 1", "ingress-no_existing-secret-host_com", build.DoNothing)
		testAction(t, projectMap, "Ing 1", "ingress-__no-service_com", build.ForceUpdate)

		testCurrentStatus(k8sClient, t, false, "default/ing1", names.NoVHostProject)
		testCurrentStatus(k8sClient, t, true, "default/ing2", names.NoVHostProject)
		testCurrentStatus(k8sClient, t, true, "default/ing1", "ingress-__foo_com")
		testCurrentStatus(k8sClient, t, false, "default/ing1", "ingress-prod_foo_com")
		testCurrentStatus(k8sClient, t, false, "default/ing1", "ingress-deprecated_foo_com")
		testCurrentStatus(k8sClient, t, false, "default/ing1", "ingress-no_existing-secret-host_com")
		testCurrentStatus(k8sClient, t, true, "default/ing1", "ingress-__no-service_com")
	})

	// 4.  Delete ingress: ing3
	t.Run("Delta_change:_Delete_ingress", func(t *testing.T) {
		deleteIng3 := &v1beta1.Ingress{}
		nsName := types.NamespacedName{Namespace: "default", Name: "ing3"}
		request = reconcile.Request{NamespacedName: nsName}
		if err := k8sClient.Get(ctx, nsName, deleteIng3); err != nil {
			t.Fatal("Error in k8s client; err: ", err)
		}
		deleteIng3.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		if err := k8sClient.Update(ctx, deleteIng3); err != nil {
			t.Fatal("Error in k8s client; err: ", err)
		}
		if _, err := r.Reconcile(request); err != nil {
			t.Error("Error building delta update")
		}

		projectMap := r.ingHandler.AdapterClient.(*gwclient.Fake).ProjectMap
		testAction(t, projectMap, "Ing 3", "ingress-__bar_com", build.ForceUpdate)
		testAction(t, projectMap, "Ing 3", "ingress-deprecated_bar_com", build.Delete)

		testCurrentStatus(k8sClient, t, false, "default/ing3", "ingress-__bar_com")
		testCurrentStatus(k8sClient, t, false, "default/ing3", "ingress-deprecated_bar_com")
	})
}

// testAction tests the action of HTTP request to the Adapter
func testAction(t *testing.T, projectsMap *build.ProjectsMap, ingName, projectName string, wantAction interface{}) {
	tp := (*projectsMap)[projectName].Action
	if tp != wantAction {
		t.Errorf("%v project: %v, action: %v; want: %v", ingName, projectName, tp, wantAction)
	}
}

// testCurrentStatus tests the current status configmap with the given ingress object and project
func testCurrentStatus(k8sClient client.Client, t *testing.T, shouldExists bool, ing, project string) {
	st, err := status.FromConfigMap(context.TODO(), &common.RequestInfo{Client: k8sClient})
	if err != nil {
		t.Fatal("Error reading status from configmap")
	}
	if shouldExists {
		if !st.ContainsProject(ing, project) {
			t.Errorf("\"%v: %v\" should exists in the current status", ing, project)
		}
	} else {
		if st.ContainsProject(ing, project) {
			t.Errorf("\"%v: %v\" should not exists in the current status", ing, project)
		}
	}
}

func readResources(path string, objType interface{}) ([]runtime.Object, error) {
	tp := reflect.TypeOf(objType)

	resource, err := readYamlResourceFile(path)
	if err != nil {
		return nil, err
	}

	res := make([]runtime.Object, 0, len(resource))
	for _, s := range resource {
		vl := reflect.New(tp)
		x := vl.Interface().(runtime.Object)
		res = append(res, x)
		if err := yaml.Unmarshal([]byte(s), x); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func readYamlResourceFile(path string) ([]string, error) {
	bytes, err := ioutil.ReadFile(filepath.FromSlash(path))
	if err != nil {
		return nil, err
	}

	s := string(bytes)
	return strings.Split(s, "\n---\n"), nil
}