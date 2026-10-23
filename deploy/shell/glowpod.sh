# Glowpod Hub shell: kubectl is set up for one cluster, as you.
alias k=kubectl
if command -v kubectl >/dev/null 2>&1; then
  source <(kubectl completion bash)
  complete -o default -F __start_kubectl k
fi
command -v helm >/dev/null 2>&1 && source <(helm completion bash)
PS1='\[\e[1m\]${GLOWPOD_CLUSTER:-glowpod}\[\e[0m\] \w \$ '
if [ -z "$GLOWPOD_GREETED" ]; then
  export GLOWPOD_GREETED=1
  echo "kubectl, helm, jq, yq and glowpod term act as you in ${GLOWPOD_CLUSTER:-this cluster}."
  echo "Files here are lost when the shell closes."
fi
