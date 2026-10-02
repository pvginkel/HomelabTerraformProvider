// Builds the homelab Terraform provider, vets and tests it, and archives it, then commits it into
// TerraformRegistry's dist/ and pushes main. That push starts IaC/TerraformRegistry, which bakes
// the mirror into the tfmirror image and pins it into TfmirrorDeploy. The push is the provider's
// only delivery path.
//
// Controller config:
//   - Job: IaC/HomelabTerraformProvider
//   - SCM: pvginkel/HomelabTerraformProvider, branch main
//   - Script Path: Jenkinsfile

library identifier: 'JenkinsPipelineUtils', changelog: false

// Set by the Build provider stage, read by the Publish provider stage.
String version

pipeline {
    agent {
        kubernetes {
            inheritFrom 'jenkins-agent-large'
            // jenkins-agent-large is YAML only, a node affinity and a toleration; without merge()
            // this file's YAML replaces it.
            yamlMergeStrategy merge()
            yaml podYaml(templates: ['iac-toolchain'], images: [[image: 'golang:1.25', name: 'go']])
        }
    }

    options {
        // Without abortPrevious: an abort between the archive and the push into TerraformRegistry
        // leaves the archived provider unpublished.
        disableConcurrentBuilds()
        skipDefaultCheckout()
        timeout(time: 60, unit: 'MINUTES')
        timestamps()
    }

    triggers {
        githubPush()
    }

    stages {
        stage('Checkout') {
            steps {
                dir('HomelabTerraformProvider') {
                    checkout scm
                }
            }
        }

        stage('Build provider') {
            steps {
                dir('HomelabTerraformProvider') {
                    script {
                        // <series>.<jenkins build>: a fresh version every build, so a consumer
                        // never sees the same version with a changed binary. version.txt holds the
                        // major.minor series (e.g. 0.1); edit it to move to 0.2.x.
                        version = "${readFile('version.txt').trim()}.${env.BUILD_NUMBER}"

                        container('go') {
                            sh 'git config --global --add safe.directory \'*\''

                            // go-ceph is cgo against librados/librbd, so the dev headers are
                            // required to compile. The runtime libs (librados2/librbd1) are needed
                            // wherever this provider is applied, not here.
                            sh 'apt-get update && apt-get install -y --no-install-recommends librados-dev librbd-dev pkg-config build-essential'

                            boolean cacheHit = sh(
                                script: 'scripts/build-cache-get.sh terraform-provider-homelab-go-mod go.sum $HOME/go/pkg/mod',
                                returnStatus: true
                            ) == 0

                            sh "CGO_ENABLED=1 go build -o terraform-provider-homelab -ldflags '-X main.version=${version}'"
                            sh 'go version -m terraform-provider-homelab'

                            if (!cacheHit) {
                                sh 'scripts/build-cache-put.sh terraform-provider-homelab-go-mod go.sum $HOME/go/pkg/mod'
                            }
                        }

                        writeJSON file: 'terraform-provider-homelab-metadata.json', json: [version: version]
                    }

                    archiveArtifacts artifacts: 'terraform-provider-homelab*', fingerprint: true
                }
            }
        }

        // Lint and Test run after Build provider: vet and the test links are cgo and need the
        // librados/librbd headers it installed into `go`. TF_ACC stays unset, so the acceptance
        // tests (TestAcc*) skip: they need live backends and are a manual run.
        stage('Lint') {
            steps {
                dir('HomelabTerraformProvider') {
                    container('go') {
                        sh 'go vet ./...'
                    }
                }
            }
        }

        stage('Test') {
            steps {
                dir('HomelabTerraformProvider') {
                    container('go') {
                        sh 'go test ./...'
                    }
                }
            }
        }

        // registry-publish.sh zips the binary, has terraform compute the h1 hash off a throwaway
        // filesystem mirror, writes index.json and <version>.json, and prunes to the newest KEEP
        // versions. It is additive and idempotent: it never changes a version a consumer's lock
        // still pins.
        stage('Publish provider') {
            steps {
                container('iac-toolchain') {
                    withCredentials([usernamePassword(
                        credentialsId: '5f6fbd66-b41c-405f-b107-85ba6fd97f10',
                        usernameVariable: 'GIT_USER',
                        passwordVariable: 'GIT_TOKEN')]) {
                        sh """
                            set -euo pipefail
                            git config --global --add safe.directory '*'

                            bin="\$PWD/HomelabTerraformProvider/terraform-provider-homelab"

                            git clone --depth 1 \
                                "https://\${GIT_USER}:\${GIT_TOKEN}@github.com/pvginkel/TerraformRegistry.git" registry
                            git -C registry config user.name  'jenkins'
                            git -C registry config user.email 'jenkins@webathome.org'

                            BIN="\$bin" VERSION="${version}" DIST="\$PWD/registry/dist" KEEP=10 \
                                HomelabTerraformProvider/scripts/registry-publish.sh

                            git -C registry add -A dist
                            if git -C registry diff --cached --quiet; then
                                echo 'registry already current; nothing to push'
                            else
                                git -C registry commit -m 'ci: publish pvginkel/homelab ${version}'
                                git -C registry push origin HEAD:main
                            fi
                        """
                    }
                }
            }
        }
    }

    post {
        aborted {
            script {
                notify.error("${env.JOB_NAME} #${env.BUILD_NUMBER} aborted (timeout or hand)")
            }
        }
    }
}
