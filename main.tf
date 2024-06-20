terraform {
  required_providers {
    kind = {
      source  = "tehcyx/kind"
      version = "~>0.5.1"
    }
    docker = {
      source  = "kreuzwerker/docker"
      version = "~>3.0.1"
    }
    shell = {
      source  = "scottwinkler/shell"
      version = "~>1.7.10"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~>2.31.0"
    }
    # We use this only for the kubectl_manifest, apparently there is a known
    # bug in `kubernetes_manifest' which prevents us from being able to recreate
    # the cluster. Thanks gavin!
    kubectl = {
      source  = "alekc/kubectl"
      version = "~>2.0.4"
    }
  }
}

provider "docker" {}

provider "shell" {
  enable_parallelism = true
}

provider "kubernetes" {
  config_path = kind_cluster.default.kubeconfig_path
}

provider "kubectl" {
  config_path = kind_cluster.default.kubeconfig_path
}

resource "kind_cluster" "default" {
  name           = "chaosmonkey-cluster"
  wait_for_ready = true
}

resource "docker_image" "chaos-monkey-image" {
  name = "chaos-monkey:dev"

  build {
    context      = path.module
    dockerfile   = "Dockerfile"
    remove       = true
    force_remove = true
  }

  force_remove = true

  triggers = {
    dockerFile = sha256(file("${path.module}/Dockerfile"))
    binFile    = sha256(filebase64("${path.module}/bin/chaos-monkey"))
  }
}

resource "shell_script" "inject-image" {
  lifecycle_commands {
    create = "kind load docker-image ${docker_image.chaos-monkey-image.name} --name ${kind_cluster.default.name} && echo '{\"json\": true}'"
    delete = "echo '{\"json\": true}'"
  }

  working_directory = path.module

  triggers = {
    image = docker_image.chaos-monkey-image.id
  }
}

resource "kubernetes_namespace" "chaosmonkey" {
  metadata {
    name = "chaosmonkey"
  }
}

resource "kubernetes_namespace" "target-namespace" {
  metadata {
    name = "target"
    labels = {
      "chaosmonkey.massix.github.io/enabled" = "enabled"
    }
  }
}

resource "kubectl_manifest" "namespace-configuration" {
  yaml_body = <<YAML
    apiVersion: cm.massix.github.io/v1alpha1
    kind: ChaosMonkeyConfiguration
    metadata:
      name: chaosmonkey-nginx
      namespace: ${kubernetes_namespace.target-namespace.id}
    spec:
      enabled: true
      minReplicas: 0
      maxReplicas: 9
      timeout: 10s
      deploymentName: nginx
  YAML

  validate_schema = false

  depends_on = [kubernetes_deployment.nginx-deployment]
}

resource "kubectl_manifest" "crd" {
  yaml_body       = file("${path.module}/crds/chaosmonkey-configuration.yaml")
  force_conflicts = true
  validate_schema = true
}

resource "kubernetes_service_account" "chaos-monkey-svcaccount" {
  metadata {
    name      = "chaosmonkey"
    namespace = kubernetes_namespace.chaosmonkey.id
  }

  automount_service_account_token = true
}

resource "kubernetes_cluster_role" "chaos-monkey-cr" {
  metadata {
    name = "chaosmonkey"
  }

  rule {
    api_groups = ["*"]
    resources  = ["namespaces"]
    verbs      = ["watch"]
  }

  rule {
    api_groups = ["*"]
    resources  = ["deployments"]
    verbs      = ["patch", "get", "scale", "update"]
  }

  rule {
    api_groups = ["*"]
    resources  = ["chaosmonkeyconfigurations"]
    verbs      = ["list", "patch", "watch"]
  }

  rule {
    api_groups = ["apps"]
    resources  = ["deployments/scale"]
    verbs      = ["update"]
  }

  rule {
    api_groups = ["*"]
    resources  = ["events"]
    verbs      = ["create", "patch"]
  }
}

resource "kubernetes_cluster_role_binding" "chaos-monkey-bind" {
  metadata {
    name = "chaosmonkey"
  }

  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.chaos-monkey-svcaccount.metadata.0.name
    namespace = kubernetes_namespace.chaosmonkey.id
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = kubernetes_cluster_role.chaos-monkey-cr.metadata.0.name
  }
}

resource "kubernetes_deployment" "chaos-monkey-deployment" {
  metadata {
    name      = "chaos-monkey"
    namespace = kubernetes_namespace.chaosmonkey.id
    labels = {
      "fr.arnal.app/name" = "chaos-monkey"
    }
    annotations = {
      "fr.arnal.app/image-id"       = docker_image.chaos-monkey-image.id
      "fr.arnal.app/dockerfile-sha" = sha256(file("${path.module}/Dockerfile"))
    }
  }

  spec {
    selector {
      match_labels = {
        "fr.arnal.app/name" = "chaos-monkey"
      }
    }

    template {
      metadata {
        labels = {
          "fr.arnal.app/name" = "chaos-monkey"
        }
      }
      spec {
        service_account_name = kubernetes_service_account.chaos-monkey-svcaccount.metadata.0.name
        container {
          name              = "chaos-monkey"
          image             = docker_image.chaos-monkey-image.name
          image_pull_policy = "Never"
        }
      }
    }
  }

  # Make sure we deploy the image before
  depends_on = [
    shell_script.inject-image,
    kubectl_manifest.crd
  ]

  # Redeploy whenever we inject a new image or change the crd
  lifecycle {
    replace_triggered_by = [
      shell_script.inject-image,
      kubectl_manifest.crd
    ]
  }
}

resource "kubernetes_deployment" "nginx-deployment" {
  metadata {
    name      = "nginx"
    namespace = kubernetes_namespace.target-namespace.id
  }

  spec {
    replicas = 1
    selector {
      match_labels = {
        "app" = "nginx"
      }
    }
    template {
      metadata {
        labels = {
          "app" = "nginx"
        }
      }
      spec {
        container {
          image = "nginx:alpine"
          name  = "nginx"
          port {
            container_port = 80
            name           = "http"
          }
        }
      }
    }
  }

  wait_for_rollout = true
}
