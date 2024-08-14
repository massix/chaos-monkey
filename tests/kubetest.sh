#!/usr/bin/env bash

KUBECTL=$(which kubectl 2>/dev/null)
CURL=$(which curl 2>/dev/null)
JQ=$(which jq 2>/dev/null)
CLUSTER_NAME="${TERRAFORM_CLUSTER_NAME:-chaosmonkey-cluster}"
DIR_PATH="$(dirname "${BASH_SOURCE[0]}")"

# shellcheck source=./tests/library.sh
source "${DIR_PATH}/library.sh"

set -eo pipefail

# Check if the cluster has been started
debug "Check that ${CLUSTER_NAME} exists"
if ! ${KUBECTL} config get-contexts | grep "kind-${CLUSTER_NAME}" &>/dev/null; then
  panic "Please start the cluster using 'make cluster-test' before running this script"
else
  debug "Force switching context to ${CLUSTER_NAME}"
  ${KUBECTL} config use-context "kind-${CLUSTER_NAME}" >/dev/null
fi
info "Cluster ${CLUSTER_NAME} found"

# Start the test
info "Starting the test"

info "Checking namespaces"
for ns in target chaosmonkey; do
  debug "Checking if namespace ${ns} target exists"
  if ! ${KUBECTL} get ns | grep ${ns} &>/dev/null; then
    panic "Namespace ${ns} does not exist"
  fi
done

info "Checking pods"
for ns in target chaosmonkey; do
  debug "Checking if pods in namespace ${ns} are ready"
  if ! ${KUBECTL} get pods --namespace=${ns} | grep Running &>/dev/null; then
    panic "Pods in namespace ${ns} are not ready"
  fi
done

info "Checking deployments"
deploymentCount=$(${KUBECTL} get deployments --namespace=chaosmonkey --no-headers | wc -l)
debug "chaosmonkey namespace contains ${deploymentCount} deployment(s)"
if [[ ${deploymentCount} != 1 ]]; then
  panic "chaosmonkey namespace should contain 1 deployment"
fi

info "Checking service"
serviceCount=$(${KUBECTL} get services --namespace=chaosmonkey --no-headers | wc -l)
debug "chaosmonkey namespace contains ${serviceCount} services"
if [[ ${serviceCount} != 1 ]]; then
  panic "chaosmonkey namespace should contain 1 service"
fi

deploymentCount=$(${KUBECTL} get deployments --namespace=target --no-headers | wc -l)
debug "target namespace contains ${deploymentCount} deployment(s)"
if [[ ${deploymentCount} != 3 ]]; then
  panic "target namespace should contain 3 deployments"
fi

info "Checking ChaosMonkeyConfigurations"
cmcCount=$(${KUBECTL} get cmc --namespace=target --no-headers | wc -l)
debug "target namespace contains ${cmcCount} cmc(s)"
if [[ ${cmcCount} != 3 ]]; then
  panic "target namespace should contain 3 cmc"
fi

disruptScale="nginx-disrupt-scale"
disruptPods="nginx-disrupt-pods"
disruptHpa="nginx-antihpa"

info "Resetting CMCs to initial values"

debug "Force enable ${disruptScale}"
${KUBECTL} -n target patch cmc chaosmonkey-${disruptScale} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null
[
  {"op": "replace", "path": "/spec/enabled", "value": true},
  {"op": "replace", "path": "/spec/scalingMode", "value": "randomScale"},
  {"op": "replace", "path": "/spec/minReplicas", "value": 2},
  {"op": "replace", "path": "/spec/maxReplicas", "value": 5}
]
JSONPATCH

debug "Force enable ${disruptPods}"
${KUBECTL} -n target patch cmc chaosmonkey-${disruptPods} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null
[
  {"op": "replace", "path": "/spec/enabled", "value": true},
  {"op": "replace", "path": "/spec/scalingMode", "value": "killPod"},
  {"op": "replace", "path": "/spec/minReplicas", "value": 6},
  {"op": "replace", "path": "/spec/maxReplicas", "value": 8}
]
JSONPATCH

debug "Force enable ${disruptHpa}"
${KUBECTL} -n target patch cmc chaosmonkey-${disruptHpa} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null
[
  {"op": "replace", "path": "/spec/enabled", "value": true},
  {"op": "replace", "path": "/spec/scalingMode", "value": "antiPressure"},
  {"op": "replace", "path": "/spec/timeout", "value": "10s"}
]
JSONPATCH

info "Resetting ${disruptPods} to 2 replicas"
${KUBECTL} -n target scale deployment ${disruptPods} --replicas=2 >/dev/null

info "Resetting ${disruptHpa} to 6 replicas"
${KUBECTL} -n target scale deployment ${disruptHpa} --replicas=6 >/dev/null

info "Checking events"
if ! ${KUBECTL} -n target get events | grep ChaosMonkey &>/dev/null; then
  warn "no events found in target namespace, please check the chaosmonkey pod logs (not considered as an error)"
fi

info "Checking CMC with scalingMode=randomScale (${disruptScale})"
replicasShouldChange -d ${disruptScale} -n target -r 5 -s 10

info "Checking CMC with scalingMode=killPod (${disruptPods})"
podsShouldChange -l "app=${disruptPods}" -n target -r 5

info "Checking CMC with scalingMode=antiPressure (${disruptHpa})"
podsShouldChange -l "app=${disruptHpa}" -n target -r 5

info "Checking number of pods for ${disruptPods}"
numberOfPodsShouldNotChange -l "app=${disruptPods}" -n target -t 2 -L 5

info "Checking number of pods for ${disruptHpa}"
numberOfPodsShouldNotChange -l "app=${disruptHpa}" -n target -t 6 -L 5

info "Stopping ${disruptScale} CMC"
if ! ${KUBECTL} patch -n target cmc chaosmonkey-${disruptScale} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
  { "op": "replace", "path": "/spec/enabled", "value": false }
]
JSONPATCH
  panic "Could not patch CMC for ${disruptScale}"
fi

info "Stopping ${disruptHpa} CMC"
if ! ${KUBECTL} patch -n target cmc chaosmonkey-${disruptHpa} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
  { "op": "replace", "path": "/spec/enabled", "value": false }
]
JSONPATCH
  panic "Could not patch CMC for ${disruptHpa}"
fi

info "Checking that CMC ${disruptScale} has been stopped correctly (number of scales should not change over time)"
replicasShouldNotChange -d ${disruptScale} -n target -l 5 -s 10

info "Checking that CMC ${disruptHpa} has been stopped correctly (pods should not change over time)"
podsShouldNotChange -l "app=${disruptHpa}" -n target -r 4 -s 10

info "Switching ${disruptPods} from scalingMode=killPod to scalingMode=randomScale"
if ! ${KUBECTL} patch -n target cmc chaosmonkey-${disruptPods} --type json --patch '[{"op":"replace", "path":"/spec/scalingMode", "value":"randomScale"}]' >/dev/null; then
  panic "Could not patch CMC ${disruptPods}"
fi

info "Checking that CMC ${disruptPods} is now correctly modifying the replicas of the deployment"
replicasShouldChange -d ${disruptPods} -n target -r 5

info "Switching ${disruptScale} from scalingMode=randomScale to scalingMode=killPod and re-enabling it"
if ! ${KUBECTL} patch -n target cmc chaosmonkey-${disruptScale} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
  { "op": "replace", "path": "/spec/enabled", "value": true },
  { "op": "replace", "path": "/spec/scalingMode", "value": "killPod" }
]
JSONPATCH
  panic "Could not patch CMC ${disruptScale}"
fi

info "Making sure there are at least two replicas of ${disruptScale}"
if ! ${KUBECTL} scale -n target deployment ${disruptScale} --replicas=2 >/dev/null; then
  panic "Could not scale ${disruptScale}"
fi

info "Checking that pods change over time"
podsShouldChange -l "app=${disruptScale}" -n target -r 5

info "Checking that we still have 2 pods"
numberOfPodsShouldNotChange -l "app=${disruptScale}" -t 2 -n target -L 5

info "Checking that chaosmonkey did not crash even once"
restartCount=$(${KUBECTL} -n chaosmonkey get pods -o jsonpath='{.items[0].status.containerStatuses[0].restartCount}')
debug "Restart count: ${restartCount}"
if [ "${restartCount}" -ne 0 ]; then
  panic "Chaosmonkey crashed :("
fi

info "Checking exposed metrics by ChaosMonkey"
debug "Opening port-forward"
${KUBECTL} port-forward -n chaosmonkey svc/chaos-monkey 9090:80 >/dev/null &

PF_PID="$!"
HOSTNAME="http://localhost:9090"
debug "port-forward pid is ${PF_PID}"
sleep 2

ALLMETRICS=(
  "chaos_monkey_nswatcher_events"
  "chaos_monkey_nswatcher_event_duration_bucket"
  "chaos_monkey_nswatcher_cmc_spawned"
  "chaos_monkey_nswatcher_cmc_active"
  "chaos_monkey_nswatcher_restarts"
  "chaos_monkey_crdwatcher_events"
  "chaos_monkey_crdwatcher_pw_spawned"
  "chaos_monkey_crdwatcher_pw_active"
  "chaos_monkey_crdwatcher_dw_spawned"
  "chaos_monkey_crdwatcher_dw_active"
  "chaos_monkey_crdwatcher_ah_spawned"
  "chaos_monkey_crdwatcher_ah_active"
  "chaos_monkey_crdwatcher_event_duration_bucket"
  "chaos_monkey_crdwatcher_restarts"
  "chaos_monkey_podwatcher_pods_added"
  "chaos_monkey_podwatcher_pods_removed"
  "chaos_monkey_podwatcher_pods_killed"
  "chaos_monkey_podwatcher_pods_active"
  "chaos_monkey_podwatcher_restarts"
  "chaos_monkey_deploymentwatcher_deployments_rescaled"
  "chaos_monkey_deploymentwatcher_random_distribution"
  "chaos_monkey_deploymentwatcher_last_scale"
  "chaos_monkey_antihpawatcher_runs"
  "chaos_monkey_antihpawatcher_pods_killed"
  "chaos_monkey_antihpawatcher_average_cpu"
)
for m in "${ALLMETRICS[@]}"; do
  metricShouldExist -m "$m" -h "localhost" -p "9090"
done

info "Checking health endpoint"
EP_RESULT=$(${CURL} -s "http://localhost:9090/health")

debug "Checking status"
if [[ "$(echo "${EP_RESULT}" | ${JQ} -r '.status')" != "up" ]]; then
  panic "Status is not ok: ${EP_RESULT}"
fi

debug "Stopping port-forward"
kill -15 ${PF_PID}

info "Check Behavior"

info "Patching namespace to disable ChaosMonkey"
if ! ${KUBECTL} patch namespace target --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
{ "op": "add", "path": "/metadata/labels", "value": {"cm.massix.github.io/namespace": "false"} },
]
JSONPATCH
  panic "Could not patch namespace"
fi

# Wait for all the ChaosMonkeys to terminate
sleep 15

info "Checking that chaosmonkey is disabled for target namespace"
podsOfNamespaceShouldNotChange -n target

info "Patching deployment to inject DenyAll"
if ! ${KUBECTL} patch -n chaosmonkey deploy chaos-monkey --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
  { "op": "replace", "path": "/spec/template/spec/containers/0/env/1/value", "value": "DenyAll" },
]
JSONPATCH
  panic "Could not patch Deployment"
fi

debug "Waiting for deployment to restart"
if ! ${KUBECTL} rollout -n chaosmonkey restart deployment chaos-monkey >/dev/null 2>/dev/null; then
  panic "Could not restart deployment"
fi

if ! ${KUBECTL} rollout -n chaosmonkey status deployment chaos-monkey >/dev/null 2>/dev/null; then
  panic "Could not wait for successful rollout"
fi

info "Checking that chaosmonkey is still disabled"
podsOfNamespaceShouldNotChange -n target

info "Patch the CMC configurations to their initial values"
if ! ${KUBECTL} -n target patch cmc chaosmonkey-${disruptScale} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
  {"op": "replace", "path": "/spec/enabled", "value": true},
  {"op": "replace", "path": "/spec/scalingMode", "value": "randomScale"},
  {"op": "replace", "path": "/spec/minReplicas", "value": 2},
  {"op": "replace", "path": "/spec/maxReplicas", "value": 4}
]
JSONPATCH
  panic "Could not patch CMC ${disruptScale}"
fi

if ! ${KUBECTL} -n target patch cmc chaosmonkey-${disruptPods} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
  {"op": "replace", "path": "/spec/enabled", "value": true},
  {"op": "replace", "path": "/spec/scalingMode", "value": "killPod"},
  {"op": "replace", "path": "/spec/minReplicas", "value": 0},
  {"op": "replace", "path": "/spec/maxReplicas", "value": 1}
]
JSONPATCH
  panic "Could not patch CMC ${disruptPods}"
fi

info "Patch the namespace to enable it"
if ! ${KUBECTL} patch namespace target --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
{ "op": "replace", "path": "/metadata/labels/cm.massix.github.io~1namespace", "value": "true" },
]
JSONPATCH
  panic "Could not patch namespace"
fi

info "Check that the pods are changing again"
podsShouldChange -l "app=${disruptPods}" -n target -r 5

info "Check that the replicas are changing again"
replicasShouldChange -d "${disruptScale}" -n target -r 5

info "Injecting older version of the CMC"
if ! ${KUBECTL} -n target delete cmc chaosmonkey-${disruptPods} >/dev/null; then
  panic "Could not delete CMC ${disruptPods}"
fi

cat <<EOF | ${KUBECTL} apply -f - >/dev/null
  apiVersion: cm.massix.github.io/v1alpha1
  kind: ChaosMonkeyConfiguration
  metadata:
    name: chaosmonkey-${disruptPods}
    namespace: target
  spec:
    minReplicas: 0
    maxReplicas: 1
    podMode: true
    timeout: 30s
    enabled: true
    deploymentName: "${disruptPods}"
EOF

# If we can get the deployment name using the new CMC we're good
deploymentName=$(${KUBECTL} -n target get cmc "chaosmonkey-${disruptPods}" -o jsonpath='{.spec.deployment.name}')
if [ "${deploymentName}" != "${disruptPods}" ]; then
  panic "Could not get CMC ${disruptPods}"
fi

info "All tests passed!"
