module nebius.ai/slurm-operator

go 1.26.1

require (
	github.com/SlinkyProject/slurm-client v1.1.0
	github.com/cucumber/godog v0.15.1
	github.com/go-logr/logr v1.4.3
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/hashicorp/go-retryablehttp v0.7.8
	github.com/hashicorp/hcl/v2 v2.22.0
	github.com/hashicorp/terraform-exec v0.24.0
	github.com/hashicorp/terraform-json v0.27.2
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/kubereboot/kured v0.0.0-20241106074119-94e73465adc3
	github.com/mackerelio/go-osstat v0.2.6
	github.com/mariadb-operator/mariadb-operator/v25 v25.10.2
	github.com/nebius/gosdk v0.0.0-20260224104345-c5b8377510ae
	github.com/onsi/ginkgo/v2 v2.28.1
	github.com/onsi/gomega v1.39.1
	github.com/openkruise/kruise-api v1.8.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.87.1
	github.com/sethvargo/go-password v0.3.1
	github.com/stretchr/testify v1.11.1
	github.com/zclconf/go-cty v1.17.0
	go.uber.org/zap v1.27.1
	golang.org/x/crypto v0.48.0
	golang.org/x/exp v0.0.0-20250408133849-7e4ce0ab07d0
	google.golang.org/grpc v1.79.3
	istio.io/pkg v0.0.0-20241216214326-d70796207df3
	k8s.io/api v0.35.2
	k8s.io/apimachinery v0.35.2
	k8s.io/client-go v0.35.2
	k8s.io/utils v0.0.0-20260210185600-b8788abfbbc2
	sigs.k8s.io/controller-runtime v0.22.4
	sigs.k8s.io/security-profiles-operator v0.8.4
	sigs.k8s.io/yaml v1.6.0
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.4-20250130201111-63bb56e20495.1 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cert-manager/cert-manager v1.18.5 // indirect
	github.com/containers/common v0.60.4 // indirect
	github.com/cucumber/gherkin/go/v26 v26.2.0 // indirect
	github.com/cucumber/messages/go/v21 v21.0.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/getkin/kin-openapi v0.133.0 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/gofrs/uuid v4.3.1+incompatible // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-memdb v1.3.4 // indirect
	github.com/hashicorp/go-version v1.8.0 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/oapi-codegen/runtime v1.2.0 // indirect
	github.com/oasdiff/yaml v0.0.0-20250309154309-f31be36b4037 // indirect
	github.com/oasdiff/yaml3 v0.0.0-20250309153720-d2182401db90 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runtime-spec v1.2.0 // indirect
	github.com/perimeterx/marshmallow v1.1.5 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/seccomp/libseccomp-golang v0.10.0 // indirect
	github.com/sethvargo/go-envconfig v1.3.0 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/woodsbury/decimal128 v1.4.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260223185530-2f722ef697dc // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/release-utils v0.8.1 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.2 // indirect
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20260115054156-294ebfa9ad83 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.9.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.41.0
	golang.org/x/term v0.40.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	golang.org/x/tools v0.41.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.34.2 // indirect
	k8s.io/klog/v2 v2.140.0
	k8s.io/kube-openapi v0.0.0-20260304202019-5b3e3fdb0acf // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
)
