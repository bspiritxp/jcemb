# Python 依赖安装指南

> 本文档说明如何初始化 Python 环境以支持 jcemb 的图片向量化功能。

## 概述

jcemb 的图片向量化通过调用本地 Python 脚本实现。系统会自动使用项目配置中的 `image.python`（默认 `python3`）来执行向量化脚本，因此你需要确保该 Python 解释器已安装所需的依赖包。

## 基础要求

- **Python 3.8+**（推荐 3.10 或更高版本）
- **pip** 包管理器

验证 Python 版本：

```bash
python3 --version
```

## 依赖包

### 1. 默认方案：OpenCLIP（推荐）

OpenCLIP 是默认的图片向量化方案，使用 `ViT-B-32` + `laion2b_s34b_b79k` 模型，生成 512 维向量。

```bash
python3 -m pip install open_clip_torch torch pillow
```

| 包名 | 用途 |
|---|---|
| `open_clip_torch` | OpenCLIP 模型加载和推理 |
| `torch` | PyTorch 深度学习框架 |
| `pillow` | 图片解码和预处理 |

### 2. 可选方案：Jina CLIP v2

如果你希望使用 Jina CLIP v2 模型（`jinaai/jina-clip-v2`），需要额外安装：

```bash
python3 -m pip install transformers torch pillow einops timm
```

| 包名 | 用途 |
|---|---|
| `transformers` | Hugging Face Transformers 库 |
| `torch` | PyTorch 深度学习框架 |
| `pillow` | 图片解码和预处理 |
| `einops` | 张量操作工具 |
| `timm` | PyTorch 图像模型库 |

**注意**：选择 Jina CLIP v2 时需要在配置中指定：

```json
{
  "image": {
    "provider": "jina-clip",
    "model": "jinaai/jina-clip-v2"
  }
}
```

### 3. 虚拟环境（推荐）

为了避免与系统 Python 包冲突，建议使用虚拟环境：

```bash
# 创建虚拟环境
python3 -m venv ~/.local/share/jcemb/image-python

# 激活虚拟环境
source ~/.local/share/jcemb/image-python/bin/activate

# 安装依赖
pip install open_clip_torch torch pillow
```

然后在 jcemb 配置中指定该虚拟环境的 Python：

```json
{
  "image": {
    "python": "/Users/<username>/.local/share/jcemb/image-python/bin/python"
  }
}
```

或使用环境变量：

```bash
export JCEMB_IMAGE_PYTHON=/Users/<username>/.local/share/jcemb/image-python/bin/python
```

## 验证安装

安装完成后，验证依赖是否正确：

```bash
python3 -c "import open_clip; import torch; import PIL; print('OK')"
```

如果没有任何错误输出，说明安装成功。

## 故障排除

### torch 安装失败（macOS）

macOS 上如果 pip 安装 torch 失败，尝试：

```bash
# 使用 conda（推荐）
conda install pytorch::pytorch torchvision -c pytorch

# 或使用 pip 指定平台
pip install torch torchvision --index-url https://download.pytorch.org/whl/cpu
```

### CUDA/GPU 支持

如果你有 NVIDIA GPU 并希望使用 CUDA 加速：

```bash
# 根据 CUDA 版本选择，例如 CUDA 11.8
pip install torch torchvision --index-url https://download.pytorch.org/whl/cu118

# 或 CUDA 12.1
pip install torch torchvision --index-url https://download.pytorch.org/whl/cu121
```

### 模型下载慢

首次使用时会自动下载模型，如果下载速度慢，可以设置镜像：

```bash
# HuggingFace 镜像（推荐）
export HF_ENDPOINT=https://hf-mirror.com

# 或设置 HuggingFace Hub 缓存目录
export HF_HOME=/path/to/cache
```

### 内存不足

图片向量化需要加载模型到内存，如果内存不足：

1. 使用 CPU 模式（默认）
2. 减少并发扫描数量：`--concurccy 1`
3. 关闭其他占用内存的应用

## 完整安装示例

```bash
# 1. 创建专用虚拟环境
python3 -m venv ~/.local/share/jcemb/image-python
source ~/.local/share/jcemb/image-python/bin/activate

# 2. 升级 pip
pip install --upgrade pip

# 3. 安装 OpenCLIP 依赖
pip install open_clip_torch torch pillow

# 4. 验证
python -c "import open_clip; import torch; print('All dependencies installed successfully')"

# 5. 配置 jcemb 使用该 Python
jcemb config
# 设置 image.python = ~/.local/share/jcemb/image-python/bin/python
```

## 依赖版本参考

以下是经过测试的兼容版本组合：

| 包 | 版本 |
|---|---|
| Python | 3.10 - 3.12 |
| torch | 2.0+ |
| open_clip_torch | 2.20+ |
| pillow | 9.0+ |
| transformers | 4.30+（Jina CLIP v2 需要） |
| einops | 0.6+（Jina CLIP v2 需要） |
| timm | 0.9+（Jina CLIP v2 需要） |

## 相关配置

更多图片相关配置参见 `jcemb config` 或手动编辑配置文件：

```json
{
  "image": {
    "provider": "openclip",
    "model": "ViT-B-32",
    "pretrained": "laion2b_s34b_b79k",
    "dimensions": 512,
    "device": "auto",
    "python": "python3",
    "vision_model": "llava"
  }
}
```

- `provider`: 图片向量 provider（`openclip` 或 `jina-clip`）
- `model`: 模型名称
- `pretrained`: 预训练权重名称（OpenCLIP 需要）
- `dimensions`: 向量维度
- `device`: 计算设备（`auto`, `cpu`, `cuda`）
- `python`: Python 解释器路径
- `vision_model`: Ollama vision model（用于图片描述生成，非向量化）
