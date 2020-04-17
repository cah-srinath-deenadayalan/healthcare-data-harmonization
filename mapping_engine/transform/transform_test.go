// Copyright 2019 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transform

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/healthcare-data-harmonization/mapping_engine/util/jsonutil" /* copybara-comment: jsonutil */
	"github.com/google/go-cmp/cmp" /* copybara-comment: cmp */
	"github.com/golang/protobuf/proto" /* copybara-comment: proto */

	dhpb "github.com/GoogleCloudPlatform/healthcare-data-harmonization/mapping_engine/proto" /* copybara-comment: data_harmonization_go_proto */
	hpb "github.com/GoogleCloudPlatform/healthcare-data-harmonization/mapping_engine/proto" /* copybara-comment: harmonization_go_proto */
	httppb "github.com/GoogleCloudPlatform/healthcare-data-harmonization/mapping_engine/proto" /* copybara-comment: http_go_proto */
	libpb "github.com/GoogleCloudPlatform/healthcare-data-harmonization/mapping_engine/proto" /* copybara-comment: library_go_proto */
	mappb "github.com/GoogleCloudPlatform/healthcare-data-harmonization/mapping_engine/proto" /* copybara-comment: mapping_go_proto */
)

type mockStorageClient struct {
	b string
}

func (s *mockStorageClient) ReadBytes(ctx context.Context, bucket string, filename string) ([]byte, error) {
	return []byte(s.b), nil
}

type mockKeyValueGCSClient struct {
	kv map[string]string
	t  *testing.T
}

func (s *mockKeyValueGCSClient) ReadBytes(ctx context.Context, bucket string, filename string) ([]byte, error) {
	if s.t != nil {
		s.t.Helper()
	}
	path := "gs://" + bucket + "/" + filename
	if v, ok := s.kv[path]; ok {
		return []byte(v), nil
	}
	if s.t != nil {
		s.t.Fatalf("tried to read path that has no value: %s", path)
	}
	return []byte{}, nil
}

func TestTransform_InitsContext(t *testing.T) {
	// This config tests two things:
	// 1) the context is initialized before any root mappings get called (so
	//    they can set variables).
	// 2) the root mappings share a context (so they can see eachother's vars).
	config := &mappb.MappingConfig{
		RootMapping: []*mappb.FieldMapping{
			{
				ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_ConstString{ConstString: "I pity the foo"}},
				Target:      &mappb.FieldMapping_TargetLocalVar{TargetLocalVar: "myFoo"},
			},
			{
				ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_FromLocalVar{FromLocalVar: "myFoo"}},
				Target:      &mappb.FieldMapping_TargetObject{TargetObject: "Foo"},
			},
		},
	}

	dhConfig := &dhpb.DataHarmonizationConfig{
		StructureMappingConfig: &hpb.StructureMappingConfig{
			Mapping: &hpb.StructureMappingConfig_MappingConfig{
				MappingConfig: config,
			},
		},
	}

	var tr *Transformer
	var err error

	if tr, err = NewTransformer(context.Background(), dhConfig); err != nil {
		t.Fatalf("could not initialize with config: %v", err)
	}

	tconfig := TransformationConfigs{
		LogTrace:     false,
		SkipBundling: false,
	}

	got, err := tr.Transform(&jsonutil.JSONContainer{}, tconfig)
	if err != nil {
		t.Fatalf("Transform({}, %v, false, false, false) got unexpected error %v", config, err)
	}

	var wantTok jsonutil.JSONToken = jsonutil.JSONArr{jsonutil.JSONStr("I pity the foo")}
	want := jsonutil.JSONContainer{"Foo": &wantTok}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Transform({}, %v, false, false, false) returned diff (-want +got):\n%s", config, diff)
	}
}

func TestTransform_NewTransformer(t *testing.T) {
	config := &mappb.MappingConfig{
		RootMapping: []*mappb.FieldMapping{
			{
				ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_ConstString{ConstString: "I pity the foo"}},
				Target:      &mappb.FieldMapping_TargetLocalVar{TargetLocalVar: "myFoo"},
			},
			{
				ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_FromLocalVar{FromLocalVar: "myFoo"}},
				Target:      &mappb.FieldMapping_TargetObject{TargetObject: "Foo"},
			},
		},
		Projector: []*mappb.ProjectorDefinition{
			{
				Name: "Patient_Patient",
				Mapping: []*mappb.FieldMapping{
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_ConstString{ConstString: "Patient"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "resourceType"},
					},
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_FromSource{FromSource: "ID"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "id"},
					},
				},
			},
		},
	}
	whistleConfig := `
	out Foo: "I pity the foo";

	def Patient_Patient(input) {
	  resourceType: "Patient";
		id: input.ID;
	}
	`

	tests := []struct {
		name       string
		config     *dhpb.DataHarmonizationConfig
		options    []Option
		want       Options
		wantCF     string
		wantErrors bool
	}{
		{
			name: "no options",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingConfig{
						MappingConfig: config,
					},
				},
			},
			options:    []Option{},
			want:       Options{},
			wantCF:     "",
			wantErrors: false,
		},
		{
			name: "enabled options",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingConfig{
						MappingConfig: config,
					},
				},
				LibraryConfig: []*libpb.LibraryConfig{
					&libpb.LibraryConfig{
						CloudFunction: []*httppb.CloudFunction{
							&httppb.CloudFunction{
								Name:       "@blah",
								RequestUrl: "https://google.cloud.function/identity",
							},
						},
					},
				},
			},
			options:    []Option{CloudFunctions(true)},
			want:       Options{CloudFunctions: true, FetchConfigs: false},
			wantCF:     "@blah",
			wantErrors: false,
		},
		{
			name: "disabled cloud functions but projector set",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingConfig{
						MappingConfig: config,
					},
				},
				LibraryConfig: []*libpb.LibraryConfig{
					&libpb.LibraryConfig{
						CloudFunction: []*httppb.CloudFunction{
							&httppb.CloudFunction{
								Name:       "@blah",
								RequestUrl: "https://google.cloud.function/identity",
							},
						},
					},
				},
			},
			options:    []Option{CloudFunctions(false)},
			want:       Options{CloudFunctions: false},
			wantCF:     "",
			wantErrors: true,
		},
		{
			name: "enable GCSClient option - whistle",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingPathConfig{
						MappingPathConfig: &hpb.MappingPathConfig{
							MappingType: hpb.MappingType_MAPPING_LANGUAGE,
							MappingConfigPath: &httppb.Location{
								Location: &httppb.Location_GcsLocation{
									GcsLocation: "gs://dummy/mapping_config.proto",
								},
							},
						},
					},
				},
			},
			options:    []Option{GCSClient(&mockStorageClient{b: whistleConfig})},
			want:       Options{},
			wantCF:     "",
			wantErrors: false,
		},
		{
			name: "enable GCSClient option - invalid whistle",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingPathConfig{
						MappingPathConfig: &hpb.MappingPathConfig{
							MappingType: hpb.MappingType_MAPPING_LANGUAGE,
							MappingConfigPath: &httppb.Location{
								Location: &httppb.Location_GcsLocation{
									GcsLocation: "gs://dummy/mapping_config.proto",
								},
							},
						},
					},
				},
			},
			options:    []Option{GCSClient(&mockStorageClient{b: "foo bar: baz"})},
			want:       Options{},
			wantCF:     "",
			wantErrors: true,
		},
		{
			name: "enable GCSClient option - whistler",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingPathConfig{
						MappingPathConfig: &hpb.MappingPathConfig{
							MappingType: hpb.MappingType_RAW_PROTO,
							MappingConfigPath: &httppb.Location{
								Location: &httppb.Location_GcsLocation{
									GcsLocation: "gs://dummy/config.wstl",
								},
							},
						},
					},
				},
			},
			options:    []Option{GCSClient(&mockStorageClient{b: proto.MarshalTextString(config)})},
			want:       Options{},
			wantCF:     "",
			wantErrors: false,
		},
		{
			name: "inline whistle",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingLanguageString{
						MappingLanguageString: whistleConfig,
					},
				},
			},
			options:    []Option{GCSClient(&mockStorageClient{b: proto.MarshalTextString(config)})},
			want:       Options{},
			wantCF:     "",
			wantErrors: false,
		},
		{
			name: "inline whistle - error",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingLanguageString{
						MappingLanguageString: "invalid whistle",
					},
				},
			},
			options:    []Option{GCSClient(&mockStorageClient{b: proto.MarshalTextString(config)})},
			want:       Options{},
			wantCF:     "",
			wantErrors: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tr, err := NewTransformer(context.Background(), test.config, test.options...)

			if test.wantErrors && err == nil {
				t.Fatalf("expected error initializing: %v", test.config)
			} else if !test.wantErrors && err != nil {
				t.Fatalf("could not initialize with config: %v", err)
			}

			if test.want.CloudFunctions && !test.wantErrors {
				_, err := tr.Registry.FindProjector(test.wantCF)
				if err != nil {
					t.Fatalf("expected cloud functions to be registered but missing in Registry: %v", err)
				}
			}
		})
	}
}

func TestTransform_NewTransformer_UserLibraries(t *testing.T) {
	config := &mappb.MappingConfig{
		RootMapping: []*mappb.FieldMapping{
			{
				ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_ConstString{ConstString: "I pity the foo"}},
				Target:      &mappb.FieldMapping_TargetLocalVar{TargetLocalVar: "myFoo"},
			},
			{
				ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_FromLocalVar{FromLocalVar: "myFoo"}},
				Target:      &mappb.FieldMapping_TargetObject{TargetObject: "Foo"},
			},
		},
		Projector: []*mappb.ProjectorDefinition{
			{
				Name: "Patient_Patient",
				Mapping: []*mappb.FieldMapping{
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_ConstString{ConstString: "Patient"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "resourceType"},
					},
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_FromSource{FromSource: "ID"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "id"},
					},
				},
			},
		},
	}

	whistleProjector := `
	def Patient_PatientWhistler(input) {
	  resourceType: "Patient";
		id: input.ID;
	}
	`

	duplicateWhistleProjector := `
	def Patient_Patient(input) {
	  resourceType: "Patient";
		id: input.ID;
	}
	`

	whistleConfig := `
	out Foo: "I pity the foo";

	def Patient_PatientWhistleConfig(input) {
	  resourceType: "Patient";
		id: input.ID;
	}
	`

	protoProjector := &mappb.MappingConfig{
		Projector: []*mappb.ProjectorDefinition{
			{
				Name: "Patient_PatientProto",
				Mapping: []*mappb.FieldMapping{
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_ConstString{ConstString: "Patient"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "resourceType"},
					},
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_FromSource{FromSource: "ID"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "id"},
					},
				},
			},
		},
	}

	duplicateProtoProjector := &mappb.MappingConfig{
		Projector: []*mappb.ProjectorDefinition{
			{
				Name: "Patient_Patient",
				Mapping: []*mappb.FieldMapping{
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_ConstString{ConstString: "Patient"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "resourceType"},
					},
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_FromSource{FromSource: "ID"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "id"},
					},
				},
			},
		},
	}

	tests := []struct {
		name                   string
		userLibs               []*libpb.UserLibrary
		gcsFiles               map[string]string
		expectedUserProjectors []string
		wantErrors             bool
	}{
		{
			name: "whistle library",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_MAPPING_LANGUAGE,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/config.wstl",
						},
					},
				},
			},
			gcsFiles:               map[string]string{"gs://dummy/config.wstl": whistleProjector},
			expectedUserProjectors: []string{"Patient_PatientWhistler"},
		},
		{
			name: "proto library",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_RAW_PROTO,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/config.textproto",
						},
					},
				},
			},
			gcsFiles:               map[string]string{"gs://dummy/config.textproto": proto.MarshalTextString(protoProjector)},
			expectedUserProjectors: []string{"Patient_PatientProto"},
		},
		{
			name: "multiple libraries",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_RAW_PROTO,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/proto.textproto",
						},
					},
				},
				&libpb.UserLibrary{
					Type: hpb.MappingType_MAPPING_LANGUAGE,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/whistler.wstl",
						},
					},
				},
			},
			gcsFiles: map[string]string{
				"gs://dummy/proto.textproto": proto.MarshalTextString(protoProjector),
				"gs://dummy/whistler.wstl":   whistleProjector,
			},
			expectedUserProjectors: []string{"Patient_PatientWhistler", "Patient_PatientProto"},
		},
		{
			name: "multiple libraries with duplicate",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_RAW_PROTO,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/proto.textproto",
						},
					},
				},
				&libpb.UserLibrary{
					Type: hpb.MappingType_MAPPING_LANGUAGE,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/whistler.wstl",
						},
					},
				},
			},
			gcsFiles: map[string]string{
				"gs://dummy/proto.textproto": proto.MarshalTextString(protoProjector),
				"gs://dummy/whistler.wstl":   duplicateWhistleProjector,
			},
			wantErrors: true,
		},
		{
			name: "whistle type but proto library",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_MAPPING_LANGUAGE,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/config.textproto",
						},
					},
				},
			},
			gcsFiles:   map[string]string{"gs://dummy/config.textproto": proto.MarshalTextString(protoProjector)},
			wantErrors: true,
		},
		{
			name: "proto type but whistle library",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_RAW_PROTO,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/config.wstl",
						},
					},
				},
			},
			gcsFiles:   map[string]string{"gs://dummy/config.wstl": whistleProjector},
			wantErrors: true,
		},
		{
			name: "whistle library - duplicate projector",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_MAPPING_LANGUAGE,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/config.wstl",
						},
					},
				},
			},
			gcsFiles:   map[string]string{"gs://dummy/config.wstl": duplicateWhistleProjector},
			wantErrors: true,
		},
		{
			name: "proto library - duplicate projector",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_RAW_PROTO,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/config.textproto",
						},
					},
				},
			},
			gcsFiles:   map[string]string{"gs://dummy/config.textproto": proto.MarshalTextString(duplicateProtoProjector)},
			wantErrors: true,
		},
		{
			name: "whistle library with more than just projector",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_MAPPING_LANGUAGE,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/config.wstl",
						},
					},
				},
			},
			gcsFiles: map[string]string{"gs://dummy/config.wstl": whistleConfig},
		},
		{
			name: "invalid type",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_INVALID,
					Path: &httppb.Location{
						Location: &httppb.Location_GcsLocation{
							GcsLocation: "gs://dummy/config.wstl",
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "remote path",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_MAPPING_LANGUAGE,
					Path: &httppb.Location{
						Location: &httppb.Location_UrlPath{
							UrlPath: "bad/location/remote",
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "local path",
			userLibs: []*libpb.UserLibrary{
				&libpb.UserLibrary{
					Type: hpb.MappingType_MAPPING_LANGUAGE,
					Path: &httppb.Location{
						Location: &httppb.Location_LocalPath{
							LocalPath: "bad/location/local",
						},
					},
				},
			},
			wantErrors: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := []Option{}
			if len(test.gcsFiles) > 0 {
				options = append(options, GCSClient(&mockKeyValueGCSClient{kv: test.gcsFiles, t: t}))
			}
			dhConfig := &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingConfig{
						MappingConfig: config,
					},
				},
				LibraryConfig: []*libpb.LibraryConfig{&libpb.LibraryConfig{UserLibraries: test.userLibs}},
			}
			transform, err := NewTransformer(context.Background(), dhConfig, options...)

			if test.wantErrors && err == nil {
				t.Fatalf("expected error initializing: %v", dhConfig)
			} else if !test.wantErrors && err != nil {
				t.Fatalf("could not initialize with config: %v", err)
			}

			for _, projector := range test.expectedUserProjectors {
				if _, err := transform.Registry.FindProjector(projector); err != nil {
					t.Errorf("expected projector %s to be registered, but got error %v", projector, err)
				}
			}
		})
	}
}

func TestTransform_JSONtoJSON(t *testing.T) {
	mconfig := &mappb.MappingConfig{
		RootMapping: []*mappb.FieldMapping{
			{
				ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_FromSource{FromSource: "."}, Projector: "Patient_Patient"},
				Target:      &mappb.FieldMapping_TargetObject{TargetObject: "Patient"},
			},
		},
		Projector: []*mappb.ProjectorDefinition{
			{
				Name: "Patient_Patient",
				Mapping: []*mappb.FieldMapping{
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_ConstString{ConstString: "Patient"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "resourceType"},
					},
					{
						ValueSource: &mappb.ValueSource{Source: &mappb.ValueSource_FromSource{FromSource: "ID"}},
						Target:      &mappb.FieldMapping_TargetField{TargetField: "id"},
					},
				},
			},
		},
	}

	dhconfig := &dhpb.DataHarmonizationConfig{
		StructureMappingConfig: &hpb.StructureMappingConfig{
			Mapping: &hpb.StructureMappingConfig_MappingConfig{
				MappingConfig: mconfig,
			},
		},
	}

	var tr *Transformer
	var err error
	if tr, err = NewTransformer(context.Background(), dhconfig); err != nil {
		t.Fatalf("could not initialize with config: %v", err)
	}

	tconfig := TransformationConfigs{
		LogTrace:     false,
		SkipBundling: false,
	}

	in := `{"ID": "test"}`
	got, err := tr.JSONtoJSON([]byte(in), tconfig)
	if err != nil {
		t.Fatalf("JSONtoJSON(%v) got expected error: %v", in, err)
	}

	want := `{"Patient":[{"id":"test","resourceType":"Patient"}]}`

	if diff := cmp.Diff(string(got), want); diff != "" {
		t.Errorf("JSONtoJSON(%v) returned diff (-want +got):\n%s", mconfig, diff)
	}
}

func TestTransform_HasPostProcessProjector(t *testing.T) {
	tests := []struct {
		name   string
		config *dhpb.DataHarmonizationConfig
		want   bool
	}{
		{
			name: "no post process",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingConfig{},
				},
			},
			want: false,
		},
		{
			name: "post process name",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingConfig{
						MappingConfig: &mappb.MappingConfig{
							PostProcess: &mappb.MappingConfig_PostProcessProjectorName{
								PostProcessProjectorName: "SomeName",
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "post process projector",
			config: &dhpb.DataHarmonizationConfig{
				StructureMappingConfig: &hpb.StructureMappingConfig{
					Mapping: &hpb.StructureMappingConfig_MappingConfig{
						MappingConfig: &mappb.MappingConfig{
							PostProcess: &mappb.MappingConfig_PostProcessProjectorDefinition{
								PostProcessProjectorDefinition: &mappb.ProjectorDefinition{},
							},
						},
					},
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tr, err := NewTransformer(context.Background(), test.config)
			if err != nil {
				t.Fatalf("could not initialize with config: %v", err)
			}
			got := tr.HasPostProcessProjector()
			if got != test.want {
				t.Errorf("HasPostProcessProjector() = %v, want %v", got, test.want)
			}
		},
		)
	}

}
