terraform {
  required_providers {
    kind = {
      source  = "tehcyx/kind"
      version = "~>0.5"
    }
    docker = {
      source  = "kreuzwerker/docker"
      version = "~>3.0"
    }
    shell = {
      source  = "scottwinkler/shell"
      version = "~>1.7"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~>2.31"
    }
    kubectl = {
      source  = "alekc/kubectl"
      version = "~>2.0"
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

  keep_locally = false

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
    certFile   = sha256(file("${path.module}/certs/chaos-monkey.chaosmonkey.svc.crt"))
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
  }
}

resource "kubectl_manifest" "deployment-mode-crd" {
  yaml_body = <<YAML
    apiVersion: cm.massix.github.io/v1
    kind: ChaosMonkeyConfiguration
    metadata:
      name: chaosmonkey-nginx-disrupt-scale
      namespace: ${kubernetes_namespace.target-namespace.id}
    spec:
      enabled: true
      minReplicas: 0
      maxReplicas: 9
      timeout: 10s
      deployment:
        name: ${kubernetes_deployment.nginx-disrupt-scale.metadata.0.name}
      scalingMode: randomScale
  YAML

  validate_schema = true

  depends_on = [
    kubernetes_deployment.nginx-disrupt-scale,
    kubernetes_deployment.chaos-monkey-deployment
  ]
}

resource "kubectl_manifest" "pods-mode-crd" {
  yaml_body = <<YAML
    apiVersion: cm.massix.github.io/v1
    kind: ChaosMonkeyConfiguration
    metadata:
      name: chaosmonkey-nginx-disrupt-pods
      namespace: ${kubernetes_namespace.target-namespace.id}
    spec:
      enabled: true
      minReplicas: 0
      maxReplicas: 9
      timeout: 10s
      deployment:
        name: ${kubernetes_deployment.nginx-disrupt-pods.metadata.0.name}
      scalingMode: killPod
  YAML

  validate_schema = true

  depends_on = [
    kubernetes_deployment.nginx-disrupt-pods,
    kubernetes_deployment.chaos-monkey-deployment
  ]
}

resource "kubectl_manifest" "pods-mode-antihpa" {
  yaml_body = <<YAML
    apiVersion: cm.massix.github.io/v1
    kind: ChaosMonkeyConfiguration
    metadata:
      name: chaosmonkey-nginx-antihpa
      namespace: ${kubernetes_namespace.target-namespace.id}
    spec:
      enabled: true
      minReplicas: 0
      maxReplicas: 4
      timeout: 10s
      deployment:
        name: ${kubernetes_deployment.nginx-anti-hpa.metadata.0.name}
      scalingMode: antiPressure
  YAML

  validate_schema = true

  depends_on = [
    kubernetes_deployment.nginx-anti-hpa,
    kubernetes_deployment.chaos-monkey-deployment
  ]
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
    api_groups = ["metrics.k8s.io"]
    resources  = ["pods"]
    verbs      = ["get", "list"]
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
    resources  = ["pods"]
    verbs      = ["get", "list", "watch", "delete"]
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
  timeouts {
    create = "30s"
    delete = "30s"
    update = "30s"
  }

  metadata {
    name      = "chaos-monkey"
    namespace = kubernetes_namespace.chaosmonkey.id
    labels = {
      "apps.massix.github.io/name" = "chaos-monkey"
    }
    annotations = {
      "apps.massix.github.io/image-id"       = docker_image.chaos-monkey-image.id
      "apps.massix.github.io/dockerfile-sha" = sha256(file("${path.module}/Dockerfile"))
    }
  }

  spec {
    selector {
      match_labels = {
        "apps.massix.github.io/name" = "chaos-monkey"
      }
    }

    template {
      metadata {
        labels = {
          "apps.massix.github.io/name" = "chaos-monkey"
        }
        annotations = {
          "prometheus.io/scrape" = "true"
          "prometheus.io/path"   = "/metrics"
          "prometheus.io/port"   = "http"
        }
      }
      spec {
        service_account_name = kubernetes_service_account.chaos-monkey-svcaccount.metadata.0.name
        container {
          name              = "chaos-monkey"
          image             = docker_image.chaos-monkey-image.name
          image_pull_policy = "Never"
          liveness_probe {
            http_get {
              port = "http"
              path = "/health"
            }
          }
          readiness_probe {
            http_get {
              port = "http"
              path = "/health"
            }
          }
          env {
            name  = "CHAOSMONKEY_LOGLEVEL"
            value = "debug"
          }
          env {
            name  = "CHAOSMONKEY_BEHAVIOR"
            value = "AllowAll"
          }
          port {
            container_port = 9000
            name           = "http"
            protocol       = "TCP"
          }
          port {
            container_port = 9443
            name           = "https"
            protocol       = "TCP"
          }
        }
      }
    }
  }

  # Make sure we deploy the image before
  depends_on = [
    shell_script.inject-image,
    kubectl_manifest.crd,
    kubectl_manifest.metrics-server
  ]

  # Redeploy whenever we inject a new image or change the crd
  lifecycle {
    replace_triggered_by = [
      shell_script.inject-image,
      kubectl_manifest.crd
    ]
  }
}

resource "kubernetes_service" "chaos-monkey" {
  metadata {
    name      = "chaos-monkey"
    namespace = kubernetes_namespace.chaosmonkey.id
  }

  spec {
    type = "ClusterIP"
    selector = {
      "apps.massix.github.io/name" = "chaos-monkey"
    }
    port {
      name        = "http"
      port        = 80
      target_port = "http"
    }
    port {
      name        = "https"
      port        = 443
      target_port = "https"
    }
  }
}

// We are going to disrupt the SCALE of this deployment
resource "kubernetes_deployment" "nginx-disrupt-scale" {
  metadata {
    name      = "nginx-disrupt-scale"
    namespace = kubernetes_namespace.target-namespace.id
  }

  spec {
    replicas = 1
    selector {
      match_labels = {
        "app" = "nginx-disrupt-scale"
      }
    }
    template {
      metadata {
        labels = {
          "app" = "nginx-disrupt-scale"
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

// We are going to disrupt the PODS of this deployment
resource "kubernetes_deployment" "nginx-disrupt-pods" {
  metadata {
    name      = "nginx-disrupt-pods"
    namespace = kubernetes_namespace.target-namespace.id
  }

  spec {
    replicas = 3
    selector {
      match_labels = {
        "app"        = "nginx-disrupt-pods"
        "otherlabel" = "othervalue"
      }
    }
    template {
      metadata {
        labels = {
          "app"        = "nginx-disrupt-pods"
          "otherlabel" = "othervalue"
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

// We are going to apply the AntiHPA scaling mode to this deployment
resource "kubernetes_deployment" "nginx-anti-hpa" {
  metadata {
    name      = "nginx-antihpa"
    namespace = kubernetes_namespace.target-namespace.id
  }

  spec {
    replicas = 6
    selector {
      match_labels = {
        app = "nginx-antihpa"
      }
    }
    template {
      metadata {
        labels = {
          app = "nginx-antihpa"
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

// Patched metrics-server
data "kubectl_file_documents" "metrics-server-manifest" {
  content = file("${path.module}/tests/manifests/components.yaml")
}

// Install the metrics-server, used for the antiHPA scaler
resource "kubectl_manifest" "metrics-server" {
  for_each         = data.kubectl_file_documents.metrics-server-manifest.manifests
  yaml_body        = each.value
  wait_for_rollout = true
}

