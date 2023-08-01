package plugin

import (
	"encoding/json"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	admissionV1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"net/http"
)

// registerRoutes takes a *http.ServeMux and registers some HTTP handlers.
func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/k8s/admission/mutation", a.CallMutation)
	mux.HandleFunc("/k8s/admission/validation", a.CallValidation)
}

// If we can implement a function, we can perhaps pass the HTTP router to it
func (a *App) CallValidation(w http.ResponseWriter, req *http.Request) {
	a.performValidationOrMutation(w, req, false)
}

func (a *App) CallMutation(w http.ResponseWriter, req *http.Request) {
	a.performValidationOrMutation(w, req, true)
}

// If we can implement a function, we can perhaps pass the HTTP router to it
func (a *App) performValidationOrMutation(w http.ResponseWriter, req *http.Request, isMutating bool) {
	request := &admissionV1.AdmissionRequest{}

	log.DefaultLogger.Info("Before decode")

	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var obj runtime.Object
	var scope conversion.Scope
	if err := runtime.Convert_runtime_RawExtension_To_runtime_Object(&request.Object, &obj, scope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	innerObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	u := unstructured.Unstructured{Object: innerObj}

	response := &admissionV1.AdmissionResponse{
		UID:              request.UID,
		Allowed:          true,
		Result:           nil,
		Patch:            nil,
		PatchType:        nil,
		AuditAnnotations: nil,
		Warnings:         nil,
	}

	if spec := u.Object["spec"]; spec != nil {
		specAsserted, _ := spec.(map[string]interface{})

		if !isMutating {
			if specAsserted["fail_validation"].(bool) {
				response.Allowed = false
				response.Result = &metav1.Status{
					TypeMeta: metav1.TypeMeta{},
					ListMeta: metav1.ListMeta{},
					Status:   "",
					Message:  "",
					Reason:   "",
					Details:  nil,
					Code:     0,
				}
			}
		} else {
			defaultFieldPatch := map[string]string{
				"op":    "add",
				"path":  "/spec/mutated_default",
				"value": "default_value",
			}

			patches := []map[string]string{
				defaultFieldPatch,
			}
			response.Patch, err = json.Marshal(patches)
			if err != nil {
				response.Result = &metav1.Status{
					TypeMeta: metav1.TypeMeta{},
					ListMeta: metav1.ListMeta{},
					Status:   "",
					Message:  "Could not translate the patch",
					Reason:   "",
					Details:  nil,
					Code:     0,
				}
			} else {
				response.Result = &metav1.Status{
					Status: "Success",
				}
			}
			pT := admissionV1.PatchTypeJSONPatch
			response.PatchType = &pT
		}

	}

	log.DefaultLogger.Info("In plugin", "isMutating", isMutating)

	w.Header().Add("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
