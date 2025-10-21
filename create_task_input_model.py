from typing import Optional, List, Union
from dataclasses import dataclass


@dataclass
class ImageUrl:
    """图片URL对象"""
    url: str
    """
    图片信息，可以是图片URL或图片 Base64 编码
    图片URL：请确保图片URL可被访问
    Base64编码：请遵循此格式data:image/<图片格式>;base64,<Base64编码>

    图片要求：
    - 格式：jpeg、png、webp、bmp、tiff、gif
    - 宽高比（宽/高）：在范围 (0.4, 2.5)
    - 宽高长度（px）：(300, 6000)
    - 大小：小于30MB
    """


@dataclass
class TextContent:
    """文本内容"""
    type: str = "text"
    """输入内容的类型，固定为 text"""

    text: str = ""
    """
    输入给模型的文本内容，描述期望生成的视频
    包括：文本提示词（必填）+ 参数（选填，格式：--[parameters]）
    建议不超过500字，支持中英文
    """


@dataclass
class ImageContent:
    """图片内容"""
    type: str = "image_url"
    """输入内容的类型，固定为 image_url"""

    image_url: ImageUrl = None
    """输入给模型的图片对象"""

    role: Optional[str] = None
    """
    图片的位置或用途
    可选值:
    - "first_frame": 首帧图片（首帧图生视频）
    - "last_frame": 尾帧图片（首尾帧图生视频）
    - "reference_image": 参考图片（参考图生视频，1-4张）

    使用场景：
    - 首帧图生视频：1个图片，role可不填或为first_frame
    - 首尾帧图生视频：2个图片，role必填，分别为first_frame和last_frame
    - 参考图生视频：1-4个图片，role必填，均为reference_image
    """


@dataclass
class SeedanceVideoGenerationInput:
    """
    火山引擎Seedance图生视频API输入模型 (doubao-seedance-1-0-lite-i2v-250428)
    创建视频生成任务API
    """

    # 必填参数
    model: str = "doubao-seedance-1-0-lite-i2v-250428"
    """模型ID，固定为 doubao-seedance-1-0-lite-i2v-250428"""

    content: List[Union[TextContent, ImageContent]] = None
    """
    输入给模型生成视频的信息，支持文本信息和图片信息
    通常包含：1个文本内容 + 1个图片内容
    """

    # 可选参数
    callback_url: Optional[str] = None
    """
    任务结果的回调通知地址
    当视频生成任务有状态变化时，方舟将向此地址推送 POST 请求
    """

    return_last_frame: Optional[bool] = False
    """
    是否返回生成视频的尾帧图像
    可选值: true (返回尾帧图像), false (不返回尾帧图像)
    """

    # 视频生成参数
    resolution: Optional[str] = "720p"
    """
    视频分辨率，简写 rs
    可选值: "480p", "720p", "1080p"
    默认值: 720p (doubao-seedance-1-0-lite-i2v)
    """

    ratio: Optional[str] = "adaptive"
    """
    生成视频的宽高比例，简写 rt
    可选值: "21:9", "16:9", "4:3", "1:1", "3:4", "9:16", "9:21", "keep_ratio", "adaptive"
    - keep_ratio: 所生成视频的宽高比与所上传图片的宽高比保持一致
    - adaptive: 根据所上传图片的比例，自动选择最合适的宽高比
    默认值: adaptive (doubao-seedance-1-0-lite-i2v)
    """

    duration: Optional[int] = 5
    """
    生成视频时长，单位：秒，简写 dur
    Seedance 支持 3~12 秒
    可选值: 3, 4, 5, 6, 7, 8, 9, 10, 11, 12
    默认值: 5秒
    """

    framespersecond: Optional[int] = 24
    """
    帧率，即一秒时间内视频画面数量，简写 fps
    可选值: 16, 24
    默认值: 24
    """

    watermark: Optional[bool] = False
    """
    生成视频是否包含水印，简写 wm
    可选值: true (含有水印), false (不含水印)
    默认值: false
    """

    seed: Optional[int] = -1
    """
    种子整数，用于控制生成内容的随机性，简写 seed
    取值范围：[-1, 2^32-1]之间的整数
    当不指定seed值或令seed取值为-1时，会使用随机数替代
    默认值: -1
    """

    camerafixed: Optional[bool] = False
    """
    是否固定摄像头，简写 cf
    可选值: true (固定摄像头), false (不固定摄像头)
    true: 平台会在用户提示词中追加固定摄像头，实际效果不保证
    默认值: false
    """