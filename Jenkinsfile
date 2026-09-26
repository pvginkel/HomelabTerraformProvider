library identifier: 'JenkinsPipelineUtils', changelog: false

podTemplate(inheritFrom: 'jenkins-agent-large', containers: [
    containerTemplate(name: 'go', image: 'golang:1.25', command: 'sleep', args: 'infinity'),
    containerTemplates.iac_toolchain('tf')
]) {
    node(POD_LABEL) {
        def version

        stage('Cloning repo') {
            dir('HomelabTerraformProvider') {
                git branch: 'main',
                    credentialsId: '5f6fbd66-b41c-405f-b107-85ba6fd97f10',
                    url: 'https://github.com/pvginkel/HomelabTerraformProvider.git'
            }
        }

        stage('Build terraform-provider-homelab') {
            dir('HomelabTerraformProvider') {
                // <series>.<jenkins build>: a fresh version every build, so a
                // consumer never sees the same version with a changed binary
                // (the mismatch that used to force a manual lock refresh).
                // version.txt holds the major.minor series (e.g. 0.1); edit it
                // to move to 0.2.x.
                version = "${readFile('version.txt').trim()}.${env.BUILD_NUMBER}"

                container('go') {
                    sh 'git config --global --add safe.directory \'*\''

                    // go-ceph is cgo against librados/librbd; the dev headers are
                    // required to compile. golang:1.25 is Debian-based, so apt
                    // works. The runtime libs (librados2/librbd1) must be present
                    // wherever this provider is *applied* — the operator's host
                    // and the consuming Ansible apply host — not here.
                    sh 'apt-get update && apt-get install -y --no-install-recommends librados-dev librbd-dev pkg-config build-essential'

                    def cacheHit = sh(
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

                archiveArtifacts artifacts: 'terraform-provider-homelab*', fingerprint: true
            }
        }

        // A vet or test failure fails the build here, so nothing reaches the
        // registry. Must run after the build stage: vet and the test links
        // are cgo and need the librados/librbd headers it installed into `go`.
        // TF_ACC stays unset — the acceptance tests (TestAcc*) skip without
        // it; they need live backends and are a manual run.
        stage('Vet and unit tests') {
            dir('HomelabTerraformProvider') {
                container('go') {
                    sh 'go vet ./...'
                    sh 'go test ./...'
                }
            }
        }

        // Append this build to the Provider Network Mirror (the dedicated
        // TerraformRegistry repo's dist/ tree) and push. That push triggers
        // the registry's pipeline, which rebuilds the nginx image and lets
        // HelmCharts redeploy it at tfmirror.home. registry-publish.sh zips
        // the binary, has terraform compute the h1 hash off a throwaway
        // filesystem mirror, writes the index.json/<version>.json, and
        // prunes to the newest KEEP versions. Runs in `tf`
        // (kube-coder-iac-toolchain: ships terraform + python3). Additive and
        // idempotent — it never mutates a version a consumer's lock still
        // pins. This is the pipeline's only delivery path.
        stage('Publish to provider registry') {
            container('tf') {
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
