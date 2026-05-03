library('JenkinsPipelineUtils') _

podTemplate(inheritFrom: 'jenkins-agent-large', containers: [
    containerTemplate(name: 'go', image: 'golang:1.25', command: 'sleep', args: 'infinity')
]) {
    node(POD_LABEL) {
        stage('Build terraform-provider-homelab') {
            dir('HomelabTerraformProvider') {
                git branch: 'main',
                    credentialsId: '5f6fbd66-b41c-405f-b107-85ba6fd97f10',
                    url: 'https://github.com/pvginkel/HomelabTerraformProvider.git'
                    
                container('go') {
                    sh 'git config --global --add safe.directory \'*\''

                    def cacheHit = sh(
                        script: 'scripts/build-cache-get.sh terraform-provider-homelab-go-mod go.sum $HOME/go/pkg/mod',
                        returnStatus: true
                    ) == 0

                    sh 'go build -o terraform-provider-homelab -ldflags "-X main.version=0.1.0"'
                    sh 'go version -m terraform-provider-homelab'

                    if (!cacheHit) {
                        sh 'scripts/build-cache-put.sh terraform-provider-homelab-go-mod go.sum $HOME/go/pkg/mod'
                    }
                }

                writeJSON file: 'terraform-provider-homelab-metadata.json', json: [version: "0.1.0"]

                archiveArtifacts artifacts: 'terraform-provider-homelab*', fingerprint: true
            }
        }

        stage('Trigger Docker image build') {
            build job: 'DockerImages', wait: false, parameters: [string(name: 'image', value: 'modern-app-dev')]
        }
    }
}
