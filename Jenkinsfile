library identifier: 'JenkinsPipelineUtils', changelog: false

podTemplate(inheritFrom: 'jenkins-agent-large', containers: [
    containerTemplate(name: 'go', image: 'golang:1.25', command: 'sleep', args: 'infinity'),
    containerTemplates.modern_app_dev('tf')
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

                    def cacheHit = sh(
                        script: 'scripts/build-cache-get.sh terraform-provider-homelab-go-mod go.sum $HOME/go/pkg/mod',
                        returnStatus: true
                    ) == 0

                    sh "go build -o terraform-provider-homelab -ldflags '-X main.version=${version}'"
                    sh 'go version -m terraform-provider-homelab'

                    if (!cacheHit) {
                        sh 'scripts/build-cache-put.sh terraform-provider-homelab-go-mod go.sum $HOME/go/pkg/mod'
                    }
                }

                writeJSON file: 'terraform-provider-homelab-metadata.json', json: [version: version]

                archiveArtifacts artifacts: 'terraform-provider-homelab*', fingerprint: true
            }
        }

        // The consuming Ansible repo pins pvginkel/homelab in its
        // terraform/{prd,scratch}/.terraform.lock.hcl. Each build mints a new
        // version, so that lock would otherwise drift; regenerate it from the
        // freshly built binary and push it back, so a fresh `iac` clone (and the
        // operator's checkout, after a pull) always has a matching lock. The
        // `tf` container is the modern-app-dev image, which ships terraform.
        stage('Update Ansible provider lock') {
            container('tf') {
                withCredentials([usernamePassword(
                    credentialsId: '5f6fbd66-b41c-405f-b107-85ba6fd97f10',
                    usernameVariable: 'GIT_USER',
                    passwordVariable: 'GIT_TOKEN')]) {
                    sh """
                        set -euo pipefail
                        git config --global --add safe.directory '*'

                        bin="\$PWD/HomelabTerraformProvider/terraform-provider-homelab"

                        # Stage the new binary into a throwaway filesystem mirror
                        # so terraform hashes it exactly as a consumer would.
                        mirror="\$(mktemp -d)"
                        dest="\$mirror/registry.terraform.io/pvginkel/homelab/${version}/linux_amd64"
                        mkdir -p "\$dest"
                        cp "\$bin" "\$dest/terraform-provider-homelab_v${version}"

                        git clone --depth 1 \
                            "https://\${GIT_USER}:\${GIT_TOKEN}@github.com/pvginkel/Ansible.git" ansible
                        git -C ansible config user.name  'jenkins'
                        git -C ansible config user.email 'jenkins@webathome.org'

                        # Rewrite only the homelab lock entry (named provider) to
                        # the new version + hash from the mirror; bpg/tls are left
                        # untouched. `-fs-mirror` overrides the image's baked
                        # /etc/terraform.rc for this command.
                        for scope in prd scratch; do
                            ( cd "ansible/terraform/\$scope"
                              terraform get -no-color
                              terraform providers lock -no-color \
                                  -fs-mirror="\$mirror" \
                                  -platform=linux_amd64 \
                                  registry.terraform.io/pvginkel/homelab )
                        done

                        git -C ansible add \
                            terraform/prd/.terraform.lock.hcl \
                            terraform/scratch/.terraform.lock.hcl
                        if git -C ansible diff --cached --quiet; then
                            echo 'homelab lock already current; nothing to push'
                        else
                            git -C ansible commit -m 'ci: bump pvginkel/homelab provider lock to ${version}'
                            git -C ansible push origin HEAD:main
                        fi
                    """
                }
            }
        }

        stage('Trigger Docker image build') {
            build job: 'DockerImages', wait: false, parameters: [string(name: 'image', value: 'modern-app-dev')]
        }
    }
}
