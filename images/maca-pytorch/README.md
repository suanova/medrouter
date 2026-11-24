# medrouter-lora

在 MetaX MACA 的 PyTorch 基础镜像上烤一层 Hugging Face 训练栈，供工作室的
"一键微调" 直接使用：任务启动即可训练，不用在 Pod 里 `pip install`。

## 版本矩阵

| 组件 | 版本 |
|---|---|
| 基础镜像 | `harbor.isuanova.com/suanova/maca-pytorch:3.8.5.2-py312-torch2.8-d11231e` |
| MACA | 3.8.5.2 |
| Python | 3.12 |
| PyTorch | 2.8 |
| transformers | 5.24.0 |
| peft | 0.22.0 |
| trl | 1.18.2 |
| datasets | 5.2.0 |
| accelerate | 1.16.0 |

这些版本是**成组升级**的：`trl 1.18` 要求 `transformers>=4.56.2`（排除 5.1.0），
`transformers 5.24` 要求 `torch>=2.6`。单独升一个很容易解不出依赖。

## 构建与推送

```sh
docker build --platform=linux/amd64 \
  -t harbor.isuanova.com/suanova/medrouter-lora:maca3.8.5-py312 \
  images/maca-pytorch

docker push harbor.isuanova.com/suanova/medrouter-lora:maca3.8.5-py312
```

构建时会做三件事，任何一件失败都让构建失败（而不是让训练任务失败）：

1. 把基础镜像里的 `torch` 版本写进 constraints，避免 pip 用 PyPI 的 CUDA 版覆盖 MACA 版；
2. `pip check` 检查依赖冲突；
3. 导入 torch / transformers / peft / trl / datasets / accelerate / pyarrow。

## 验证

```sh
docker run --rm --entrypoint python3 \
  harbor.isuanova.com/suanova/medrouter-lora:maca3.8.5-py312 \
  -c "import trl, peft, transformers; print(trl.__version__, peft.__version__)"
```

在集群里带 GPU 验证（需要 `metax-tech.com/gpu` 资源）：

```sh
kubectl run lora-smoke --restart=Never \
  --image=harbor.isuanova.com/suanova/medrouter-lora:maca3.8.5-py312 \
  --overrides='{"spec":{"containers":[{"name":"lora-smoke","image":"harbor.isuanova.com/suanova/medrouter-lora:maca3.8.5-py312","resources":{"limits":{"metax-tech.com/gpu":"1"}},"command":["python3","-c","import torch;print(torch.__version__, torch.cuda.is_available(), torch.cuda.device_count())"]}]}}'
kubectl logs lora-smoke && kubectl delete pod lora-smoke
```

## 故意不装的东西

| 包 | 原因 |
|---|---|
| `bitsandbytes` | 只支持 NVIDIA CUDA，MetaX 上无法工作。QLoRA（4-bit）用不了，配方统一走 bf16 LoRA |
| `deepspeed` | 需要针对 CUDA 编译，且当前只有单机 2 卡；等有多机需求再单独做镜像 |
| `flash-attn` | 同上，需要与 MACA 版本匹配的预编译包 |
| `torchvision` / `torchaudio` | 文本微调用不到；基础镜像若已带则保留 |

## 与节点驱动的兼容性

集群节点驱动是 MACA **3.9.6**，镜像是 **3.8.5.2**。MACA 通常向后兼容
（现有推理 profile 用 3.8.2 也在同一节点运行），但这是第一次跑训练时要验证的点：
若出现 `libmaca` 加载失败，需要按节点的驱动版本重烤基础镜像。
