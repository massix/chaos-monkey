#!/usr/bin/env bash

# Very simple logging library to be used with kubetest.sh

function log() {
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

function info() {
  log info "$*"
}

function warn() {
  log warn "$*"
}

function debug() {
  if [[ "${TEST_DEBUG:-false}" == "true" ]]; then
    log debug "$*"
  fi
}

function panic() {
  log error "$*"

  if [[ "${PF_PID}" != "" ]]; then
    info "Force stopping port-forward after failure"
    kill -15 "${PF_PID}"
  fi

  exit 1
}
