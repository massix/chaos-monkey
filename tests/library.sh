#!/usr/bin/env bash
DIR_PATH="$(dirname "${BASH_SOURCE[0]}")"

# shellcheck source=./tests/log.sh
source "${DIR_PATH}/log.sh"

# Common functions to be reused in tests

# Checks that a program exists in the PATH
# @param programName
# @param errorMessage
# @returns 0 if the program exists, panics otherwise
function checkProgram() {
  local programName=""
  local errorMessage=""

  if [ $# -lt 2 ]; then
    panic "checkProgram expects at least 2 arguments, got $#"
  fi

  programName="$1"
  shift
  errorMessage="$*"

  debug "Checking ${programName}"

  if ! type "${programName}" 2>/dev/null >/dev/null; then
    panic "${errorMessage}"
  fi
}

# Check that we have everything we need to run the tests
checkProgram kubectl "Please install kubectl: https://kubernetes.io/docs/tasks/tools/install-kubectl/"
checkProgram curl "Please install curl: https://curl.se/download.html"

# Checks that the scale value of a given deployment change over time
# @param -d deploymentName (mandatory), the name of the deployment to watch
# @param -n namespace (default: ""), the namespace of the deployment
# @param -r retries (default: 10), the maximum amount of retries
# @param -s sleep (default: 10), the amount of time to sleep in seconds between retries
function replicasShouldChange() {
  local OPTIND OPTARG

  local deploymentName=""
  local namespace=""
  local -i maxRetries=10
  local -i sleepDuration=10

  local -i currentReplicas
  local -i newReplicas
  local -i completedLoops
  local -i testSuccess
  local -r jsonPath="{.spec.replicas}"

  while getopts "d:r:n:s:" opt; do
    case "${opt}" in
    d)
      deploymentName="${OPTARG}"
      ;;
    r)
      maxRetries="${OPTARG}"
      ;;
    n)
      namespace="${OPTARG}"
      ;;
    s)
      sleepDuration="${OPTARG}"
      ;;
    "?")
      panic "Invalid option: -${OPTARG}"
      ;;
    esac
  done

  info "Checking that replicas for ${deploymentName} change over time (max ${maxRetries} retries)"
  debug "replicasShouldChange() for ${deploymentName}, at most ${maxRetries} retries, namespace: ${namespace}, sleep: ${sleepDuration}"
  currentReplicas=$(kubectl get deployment "${deploymentName}" --namespace "${namespace}" -o jsonpath="${jsonPath}")
  newReplicas=${currentReplicas}
  testSuccess=0

  while [[ ${completedLoops} -lt ${maxRetries} ]]; do
    debug "Current replicas: ${currentReplicas}, waiting for replicas to change ($((maxRetries - completedLoops)) retries left)"
    newReplicas=$(kubectl get deployment "${deploymentName}" --namespace "${namespace}" -o jsonpath="${jsonPath}")
    debug "Current replicas: ${currentReplicas}, new replicas: ${newReplicas}"

    completedLoops=$((completedLoops + 1))

    if [ ${newReplicas} != ${currentReplicas} ]; then
      debug "Replicas have changed, leaving loop"
      testSuccess=1
      break
    fi

    sleep ${sleepDuration}
  done

  if [[ $testSuccess -eq 0 ]]; then
    panic "Replicas did not change after ${completedLoops} retries."
  fi
}

# Checks that the replica value of a given deployment does not change over time
# @param -d deploymentName (mandatory), the name of the deployment to watch
# @param -n namespace (default: ""), the namespace of the deployment
# @param -l loops (default: 5), the amount of loops to complete
# @param -s sleep (default: 10), the amount of time to sleep in seconds between loops
function replicasShouldNotChange() {
  local OPTIND OPTARG

  local deploymentName=""
  local -i loopsToComplete=5
  local namespace""
  local -i sleepDuration=10

  local -i currentReplicas
  local -i newReplicas
  local -i completedLoops
  local -r jsonPath="{.spec.replicas}"

  while getopts "d:l:n:s:" opt; do
    case "${opt}" in
    d)
      deploymentName="${OPTARG}"
      ;;
    l)
      loopsToComplete="${OPTARG}"
      ;;
    n)
      namespace="${OPTARG}"
      ;;
    s)
      sleepDuration="${OPTARG}"
      ;;
    "?")
      panic "Invalid option: -${OPTARG}"
      ;;
    esac
  done

  info "Checking that replicas for ${deploymentName} do not change (${loopsToComplete} loops)"

  debug "replicasShouldNotChange() for ${deploymentName}, must complete ${loopsToComplete} loops, namespace: ${namespace}, sleep: ${sleepDuration}"
  currentReplicas=$(kubectl get deployment "${deploymentName}" --namespace "${namespace}" -o jsonpath="${jsonPath}")
  newReplicas=${currentReplicas}

  while [[ ${completedLoops} -lt ${loopsToComplete} ]]; do
    debug "Current replicas: ${currentReplicas}, replicas should not change ($((loopsToComplete - completedLoops)) loops left)"
    newReplicas=$(kubectl get deployment "${deploymentName}" --namespace "${namespace}" -o jsonpath="${jsonPath}")
    debug "Current replicas: ${currentReplicas}, new replicas: ${newReplicas}"

    if [ "${currentReplicas}" != "${newReplicas}" ]; then
      panic "Replicas changed (${currentReplicas} -> ${newReplicas})"
    else
      debug "Still ok, replicas: ${currentReplicas} ($((loopsToComplete - completedLoops)) loops left)"
      sleep ${sleepDuration}
    fi

    completedLoops=$((completedLoops + 1))
  done
}

# Helper function to check that the pods of a given deployment are recreated over time
# @params -l selector (mandatory), the selector of the deployment
# @params -n namespace (mandatory), the namespace of the deployment
# @params -r retries (default: 10), maximum number of retries
# @params -s sleep (default: 10), seconds to sleep between one loop and the other
function podsShouldChange() {
  local OPTIND OPTARG

  local selector=""
  local namespace=""
  local -i retries=10
  local -i sleepDuration=10

  local -i completedLoops=0
  local -r jsonPath="{.items[*].metadata.name}"
  local -i testSuccess=0
  local currentPods
  local newPods

  while getopts "l:n:r:s:" opt; do
    case "${opt}" in
    l)
      selector="${OPTARG}"
      ;;
    n)
      namespace="${OPTARG}"
      ;;
    r)
      retries="${OPTARG}"
      ;;
    s)
      sleepDuration="${OPTARG}"
      ;;
    "?")
      panic "Invalid option: -${OPTARG}"
      ;;
    esac
  done

  currentPods=$(kubectl get pods -n "${namespace}" --selector "${selector}" -o jsonpath="${jsonPath}")
  newPods=${currentPods}

  debug "podsShouldChange() for ${selector}, max ${retries} retries, namespace: ${namespace}, sleep: ${sleepDuration}"

  completedLoops=0

  info "Checking that pods for ${selector} are changing"

  while [[ ${completedLoops} -lt ${retries} ]]; do
    newPods=$(kubectl get pods -n "${namespace}" --selector "${selector}" -o jsonpath="${jsonPath}")
    debug "Current pods: ${currentPods}, new pods: ${newPods}"
    completedLoops=$((completedLoops + 1))

    if [ "${newPods}" != "${currentPods}" ]; then
      testSuccess=1
      break
    fi

    debug "Pods for \"${selector}\" are still the same ($((retries - completedLoops)) retries left)"
    sleep ${sleepDuration}
  done

  if [[ ${testSuccess} -eq 0 ]]; then
    panic "Pods for \"${selector}\" did not change after ${retries} retries"
  fi
}

# Checks that the number of pods do not change over time
# @param -l selector (mandatory), the selector of the deployment
# @param -n namespace (default: ""), the namespace of the deployment
# @param -t targetValue (mandatory), the expected number of pods
# @param -L Loops (default: 10), loops to complete
# @param -s sleep (default: 10), seconds to sleep between one loop and the other
function numberOfPodsShouldNotChange() {
  local OPTIND OPTARG

  local selector=""
  local -i targetValue=0
  local -i loopsToComplete=10
  local -i sleepDuration=10
  local namespace=""

  local -i countPods
  local -i completedLoops

  while getopts "l:n:t:L:s:" opt; do
    case "${opt}" in
    l)
      selector="${OPTARG}"
      ;;
    n)
      namespace="${OPTARG}"
      ;;
    t)
      targetValue="${OPTARG}"
      ;;
    L)
      loopsToComplete="${OPTARG}"
      ;;
    s)
      sleepDuration="${OPTARG}"
      ;;
    "?")
      panic "Invalid option: -${OPTARG}"
      ;;
    esac
  done

  debug "numberOfPodsShouldNotChange() for ${selector}, max ${loopsToComplete} retries, namespace: ${namespace}, sleep: ${sleepDuration}"

  completedLoops=0

  info "Checking number of pods for ${selector} (should stay ${targetValue} for ${loopsToComplete} loops)"
  countPods=$(kubectl get -n "${namespace}" pods --selector "${selector}" --no-headers | wc -l)

  while [[ ${completedLoops} -lt ${loopsToComplete} ]]; do
    if [[ ${countPods} -ne ${targetValue} ]]; then
      panic "There should always be ${targetValue} pods active"
    fi

    debug "Number of pods still ${countPods} ($((loopsToComplete - completedLoops)) loops left)"
    completedLoops=$((completedLoops + 1))
    sleep ${sleepDuration}
  done
}

# Check that all the pods of a given namespace do not change over time
# @param -n namespace (mandatory), the namespace to check
# @param -s sleep (default: 10), seconds to sleep between one loop and the other
# @param -l loop (default: 5), number of loops to complete
function podsOfNamespaceShouldNotChange() {
  local OPTIND OPTARG

  local -i completedLoops=0
  local -i sleepDuration=10
  local cpList
  local newCpList
  local namespace=""
  local -i loopsToComplete=5
  local -r jsonPath="{.items[*].metadata.name}"

  while getopts "n:s:l:" opt; do
    case "${opt}" in
    n)
      namespace="${OPTARG}"
      ;;
    s)
      sleepDuration="${OPTARG}"
      ;;
    l)
      loopsToComplete="${OPTARG}"
      ;;
    "?")
      panic "Invalid option: -${OPTARG}"
      ;;
    esac
  done

  cpList=$(kubectl get pods -n "${namespace}" -o jsonpath="${jsonPath}")

  info "Checking that all the pods of namespace ${namespace} do not change over time (${loopsToComplete} loops)"

  while [[ ${completedLoops} -lt ${loopsToComplete} ]]; do
    newCpList=$(kubectl get pods -n "${namespace}" -o jsonpath="${jsonPath}")

    if [[ "${cpList}" == "${newCpList}" ]]; then
      debug "Pods did not change ($((loopsToComplete - completedLoops)) loops left)"
      sleep ${sleepDuration}
    else
      panic "Pods have changed"
    fi

    completedLoops=$((completedLoops + 1))
  done
}

# Checks that a given metric exist for ChaosMonkey
# *WARNING*: it is the job of the caller to ensure that the port-forward is running!
# @param -m metricName (mandatory), the name of the metric
# @param -h hostname (mandatory), the hostname of the target
# @param -p port (mandatory), the port of the target
# @param -s scheme (default: "http"), the scheme of the target
# @param -P metricsPath (default: "metrics"), the path of the metrics
function metricShouldExist() {
  local OPTIND OPTARG

  local metricName=""
  local hostname=""
  local port=""
  local scheme="http"
  local metricsPath="metrics"

  while getopts "m:h:p:s:P:" opt; do
    case "${opt}" in
    m)
      metricName="${OPTARG}"
      ;;
    h)
      hostname="${OPTARG}"
      ;;
    p)
      port="${OPTARG}"
      ;;
    s)
      scheme="${OPTARG}"
      ;;
    P)
      metricsPath="${OPTARG}"
      ;;
    "?")
      panic "Invalid option: -${OPTARG}"
      ;;
    esac
  done

  info "Checking presence of metric ${metricName} @ ${scheme}/${hostname}:${port}/${metricsPath}"
  if ! curl -s "${scheme}://${hostname}:${port}/${metricsPath}" | grep "${metricName}" 2>/dev/null >/dev/null; then
    panic "Metric ${metricName} not found on ${scheme}://${hostname}:${port}/${metricsPath}"
  fi
}
