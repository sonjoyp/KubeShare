set -ex

#Install nvidia driver if needed
#sudo apt install nvidia-driver-435
#sudo reboot

sudo swapoff -a && sudo sed -i 's/\/swap/#\/swap/g' /etc/fstab

sudo apt-get update && sudo apt-get install apt-transport-https ca-certificates curl software-properties-common

curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -

# Add Docker's official GPG key:
sudo apt-get update
sudo apt-get install ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

# Add the repository to Apt sources:
echo \
  "deb [arch="$(dpkg --print-architecture)" signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  "$(. /etc/os-release && echo "$VERSION_CODENAME")" stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update

sudo apt-get install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# for verification
sudo docker run hello-world

#Install k8s
curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo apt-key add –
sudo apt-get update
sudo apt-add-repository "deb http://apt.kubernetes.io/ kubernetes-xenial main"
sudo apt install -y kubeadm=1.18.10-00 kubelet=1.18.10-00 kubectl=1.18.10-00
sudo apt-mark hold kubelet kubeadm kubectl

#configure docker, nvidia-docker-plugin, etc
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg \
  && curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
	sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
	sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list \
  && \
	sudo apt-get update


cat <<EOF | sudo tee /etc/docker/daemon.json
{
 "exec-opts": ["native.cgroupdriver=systemd"],
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "100m"
  },
  "storage-driver": "overlay2",
  "default-runtime": "nvidia",
    "runtimes": {
        "nvidia": {
            "path": "/usr/bin/nvidia-container-runtime",
            "runtimeArgs": []
        }
    }
}
EOF

sudo systemctl daemon-reload
sudo systemctl restart docker

sudo snap install go --channel=1.16/stable --classic

sudo kubeadm init

mkdir -p $HOME/.kube
sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
sudo chown "$(id -u):$(id -g)" $HOME/.kube/config

kubectl apply -f https://docs.projectcalico.org/v3.18/manifests/calico.yaml

kubectl taint nodes --all node-role.kubernetes.io/master-
kubectl taint nodes --all node-role.kubernetes.io/control-plane-

#https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/sample-workload.html
#https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html
kubectl create -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.14.1/nvidia-device-plugin.yml

#https://stackoverflow.com/questions/74741993/0-1-nodes-are-available-1-pod-has-unbound-immediate-persistentvolumeclaims
helm install prometheus prometheus-community/prometheus --namespace prometheus --version 19.7.2
export POD_NAME=$(kubectl get pods --namespace prometheus -l "app=prometheus,component=server" -o jsonpath="{.items[0].metadata.name}")
kubectl --namespace prometheus port-forward $POD_NAME 9090

#git clone https://github.com/sonjoyp/KubeShare.git --branch release-1.0
#cd KubeShare
#make
#cd ./deploy
#kubectl apply -f .

git clone https://github.com/sonjoyp/KubeShare.git --branch release-2.0
cd KubeShare
make