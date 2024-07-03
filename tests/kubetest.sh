#!/usr/bin/env bash

KUBECTL=$(which kubectl)
CLUSTER_NAME="${TERRAFORM_CLUSTER_NAME:-chaosmonkey-cluster}"
CURL=$(which curl)

set -eo pipefail

log() {
  local level="${1}"
  local coloredLevel=""
  shift
  case "${level}" in
  info)
    coloredLevel="\033[0;32m${level}\033[0m"
    ;;
  warn)
    coloredLevel="\033[0;33m${level}\033[0m"
    ;;
  error)
    coloredLevel="\033[0;31m${level}\033[0m"
    ;;
  debug)
    coloredLevel="\033[0;34m${level}\033[0m"
    ;;
  *)
    coloredLevel="\033[0;32munknown\033[0m"
    ;;

  esac

  echo -e "[$(date +"%Y-%m-%d %H:%M:%S")] [${coloredLevel}] - $*"
}

info() {
  log info "$*"
}

warn() {
  log warn "$*"
}

debug() {
  if [[ "${TEST_DEBUG:-false}" == "true" ]]; then
    log debug "$*"
  fi
}

err() {
  log error "$*"

  if [[ "${PF_PID}" != "" ]]; then
    info "Force stopping port-forward after failure"
    kill -15 "${PF_PID}"
  fi

  exit 1
}

# Helper function to check that the replicas of a given deployment change over time
checkReplicas() {
  local deploymentName="$1"
  local currentScale
  local newScale
  local completedLoops

  currentScale=$(${KUBECTL} get deployment "${deploymentName}" --namespace=target -o jsonpath='{.spec.replicas}')
  newScale=$(${KUBECTL} get deployment "${deploymentName}" --namespace=target -o jsonpath='{.spec.replicas}')
  debug "Checking replicas for ${deploymentName}"

  completedLoops=0
  while [ "${currentScale}" == "${newScale}" ]; do
    info "Current replicas: ${currentScale}, waiting for the replicas to change ($((10 - completedLoops)) retries left)"

    newScale=$(${KUBECTL} get deployment "${deploymentName}" --namespace=target -o jsonpath='{.spec.replicas}')
    debug "Current replicas: ${currentScale}, new replicas: ${newScale}"

    completedLoops=$((completedLoops + 1))
    if [ ${completedLoops} -gt 10 ]; then
      err "Replicas did not change after ${completedLoops} retries, please check the chaosmonkey pod logs"
    else
      sleep 10
    fi
  done
}

# Helper function to verify that the scale of a given deployment *does not* change over time
checkReplicasDoesNotChange() {
  local deploymentName="$1"
  local currentScale
  local newScale
  local completedLoops

  debug "Checking that .spec.replicas for ${deploymentName} does not change over time"

  currentScale=$(${KUBECTL} get deployment "${deploymentName}" --namespace=target -o jsonpath='{.spec.replicas}')
  newScale=$(${KUBECTL} get deployment "${deploymentName}" --namespace=target -o jsonpath='{.spec.replicas}')

  completedLoops=0
  while [ ${completedLoops} -lt 5 ]; do
    debug "Loop #${completedLoops}"

    if [ "${currentScale}" != "${newScale}" ]; then
      err "Number of replicas changed (${currentScale} -> ${newScale})"
    else
      info "Still ok, number of replicas: ${currentScale} ($((5 - completedLoops)) loops left)"
      sleep 5
      newScale=$(${KUBECTL} get deployment "${deploymentName}" --namespace=target -o jsonpath='{.spec.replicas}')
      completedLoops=$((completedLoops + 1))
    fi
  done
}

# Helper function to check that the pods of a given deployment are recreated over time
checkPods() {
  local selector="$1"
  local currentPods
  local newPods
  local completedLoops

  currentPods=$(${KUBECTL} get -n target pods --selector "${selector}" -o jsonpath='{.items[*].metadata.name}')
  newPods=$(${KUBECTL} get -n target pods --selector "${selector}" -o jsonpath='{.items[*].metadata.name}')

  debug "Checking pods for ${selector}"

  completedLoops=0
  while [ "${currentPods}" == "${newPods}" ]; do
    info "Current pods: ${currentPods}, waiting for the pods to change ($((10 - completedLoops)) retries left)"

    newPods=$(${KUBECTL} get -n target pods --selector "${selector}" -o jsonpath='{.items[*].metadata.name}')
    debug "Current pods: ${currentPods}, new pods: ${newPods}"

    completedLoops=$((completedLoops + 1))
    if [ ${completedLoops} -gt 10 ]; then
      err "Pods did not change after ${completedLoops} retries, please check the chaosmonkey pod logs"
    else
      sleep 10
    fi
  done
}

# Helper function to check that the number of pods stay equal over time
checkNumberPods() {
  local selector="$1"
  local targetValue=$2
  local countPods
  local completedLoops

  debug "Checking number of pods for ${selector} (should remain ${targetValue})"

  countPods=$(${KUBECTL} get -n target pods --selector "${selector}" --no-headers | wc -l)

  completedLoops=0
  info "Checking number of pods"
  while [[ ${countPods} != "${targetValue}" ]]; do
    debug "Checking number of pods (${countPods}), waiting for ${targetValue} ($((10 - completedLoops)) retries left)"
    countPods=$(${KUBECTL} get -n target pods --selector "${selector}" --no-headers | wc -l)
    sleep 1

    completedLoops=$((completedLoops + 1))
    if [ ${completedLoops} -gt 10 ]; then
      err "There should always be 2 pods active"
    fi
  done
}

checkMetric() {
  local hostname="$1"
  local metricName="$2"

  info "Checking presence of metric $metricName @ $hostname"
  if ! ${CURL} -s "${hostname}/metrics" | grep "${metricName}" 2>/dev/null >/dev/null; then
    err "Metric ${metricName} not found on ${hostname}"
  fi
}

debug "Checking kubectl @ ${KUBECTL}"
if [[ -z "${KUBECTL}" ]]; then
  err "Please install kubectl: https://kubernetes.io/docs/tasks/tools/install-kubectl/"
fi
info "Kubectl found at ${KUBECTL}"

debug "Checking curl @ ${CURL}"
if [[ -z "${CURL}" ]]; then
  err "Please install curl: https://curl.se/download.html"
fi
info "Curl found at ${CURL}"

# Check if the cluster has been started
debug "Check that ${CLUSTER_NAME} exists"
if ! ${KUBECTL} config get-contexts | grep "kind-${CLUSTER_NAME}" &>/dev/null; then
  err "Please start the cluster using 'make cluster-test' before running this script"
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
    err "Namespace ${ns} does not exist"
  fi
done

info "Checking pods"
for ns in target chaosmonkey; do
  debug "Checking if pods in namespace ${ns} are ready"
  if ! ${KUBECTL} get pods --namespace=${ns} | grep Running &>/dev/null; then
    err "Pods in namespace ${ns} are not ready"
  fi
done

info "Checking deployments"
deploymentCount=$(${KUBECTL} get deployments --namespace=chaosmonkey --no-headers | wc -l)
debug "chaosmonkey namespace contains ${deploymentCount} deployment(s)"
if [[ ${deploymentCount} != 1 ]]; then
  err "chaosmonkey namespace should contain 1 deployment"
fi

info "Checking service"
serviceCount=$(${KUBECTL} get services --namespace=chaosmonkey --no-headers | wc -l)
debug "chaosmonkey namespace contains ${serviceCount} services"
if [[ ${serviceCount} != 1 ]]; then
  err "chaosmonkey namespace should contain 1 service"
fi

deploymentCount=$(${KUBECTL} get deployments --namespace=target --no-headers | wc -l)
debug "target namespace contains ${deploymentCount} deployment(s)"
if [[ ${deploymentCount} != 2 ]]; then
  err "target namespace should contain 2 deployments"
fi

info "Checking ChaosMonkeyConfigurations"
cmcCount=$(${KUBECTL} get cmc --namespace=target --no-headers | wc -l)
debug "target namespace contains ${cmcCount} cmc(s)"
if [[ ${cmcCount} != 2 ]]; then
  err "target namespace should contain 2 cmc"
fi

disruptScale="nginx-disrupt-scale"
disruptPods="nginx-disrupt-pods"

info "Resetting CMCs to initial values"

debug "Force enable ${disruptScale}"
${KUBECTL} -n target patch cmc chaosmonkey-${disruptScale} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null
[
  {"op": "replace", "path": "/spec/enabled", "value": true},
  {"op": "replace", "path": "/spec/podMode", "value": false},
  {"op": "replace", "path": "/spec/minReplicas", "value": 2},
  {"op": "replace", "path": "/spec/maxReplicas", "value": 5}
]
JSONPATCH

debug "Force enable ${disruptPods}"
${KUBECTL} -n target patch cmc chaosmonkey-${disruptPods} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null
[
  {"op": "replace", "path": "/spec/enabled", "value": true},
  {"op": "replace", "path": "/spec/podMode", "value": true},
  {"op": "replace", "path": "/spec/minReplicas", "value": 6},
  {"op": "replace", "path": "/spec/maxReplicas", "value": 8}
]
JSONPATCH

info "Resetting ${disruptPods} to 2 replicas"
${KUBECTL} -n target scale deployment ${disruptPods} --replicas=2 >/dev/null

info "Checking events"
if ! ${KUBECTL} -n target get events | grep ChaosMonkey &>/dev/null; then
  warn "no events found in target namespace, please check the chaosmonkey pod logs (not considered as an error)"
fi

info "Checking CMC with podMode=false (${disruptScale})"
checkReplicas ${disruptScale}

info "Checking CMC with podMode=true (${disruptPods})"
checkPods "app=${disruptPods}"

info "Checking number of pods"
checkNumberPods "app=${disruptPods}" 2

info "Stopping ${disruptScale} CMC"
if ! ${KUBECTL} patch -n target cmc chaosmonkey-${disruptScale} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
  { "op": "replace", "path": "/spec/enabled", "value": false }
]
JSONPATCH
  err "Could not patch CMC for ${disruptScale}"
fi

info "Checking that CMC ${disruptScale} has been stopped correctly (number of scales should not change over time)"
checkReplicasDoesNotChange ${disruptScale}

info "Switching ${disruptPods} from podMode=true to podMode=false"
if ! ${KUBECTL} patch -n target cmc chaosmonkey-${disruptPods} --type json --patch '[{"op":"replace", "path":"/spec/podMode", "value":false}]' >/dev/null; then
  err "Could not patch CMC ${disruptPods}"
fi

info "Checking that CMC ${disruptPods} is now correctly modifying the replicas of the deployment"
checkReplicas ${disruptPods}

info "Switching ${disruptScale} from podMode=false to podMode=true and re-enabling it"
if ! ${KUBECTL} patch -n target cmc chaosmonkey-${disruptScale} --type json --patch-file=/dev/stdin <<-JSONPATCH >/dev/null; then
[
  { "op": "replace", "path": "/spec/enabled", "value": true },
  { "op": "replace", "path": "/spec/podMode", "value": true }
]
JSONPATCH
  err "Could not patch CMC ${disruptScale}"
fi

info "Making sure there are at least two replicas of ${disruptScale}"
if ! ${KUBECTL} scale -n target deployment ${disruptScale} --replicas=2 >/dev/null; then
  err "Could not scale ${disruptScale}"
fi

info "Checking that pods change over time"
checkPods "app=${disruptScale}"

info "Checking that we still have 2 pods"
checkNumberPods "app=${disruptScale}" 2

info "Checking that chaosmonkey did not crash even once"
restartCount=$(${KUBECTL} -n chaosmonkey get pods -o jsonpath='{.items[0].status.containerStatuses[0].restartCount}')
debug "Restart count: ${restartCount}"
if [ "${restartCount}" -ne 0 ]; then
  err "Chaosmonkey crashed :("
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
)
for m in "${ALLMETRICS[@]}"; do
  checkMetric $HOSTNAME "$m"
done

debug "Stopping port-forward"
kill -15 ${PF_PID}

info "All tests passed!"
