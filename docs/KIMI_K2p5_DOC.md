# Kimi K2.5

## Overview of Kimi K2.5 Model

Kimi K2.5 is Kimi's most intelligent model to date, achieving open-source SoTA performance in Agent, code, visual understanding, and a range of general intelligent tasks. It is also Kimi's most versatile model to date, featuring a native multimodal architecture that supports both visual and text input, thinking and non-thinking modes, and dialogue and Agent tasks. [Tech Blog](https://api.moonshot.ai/docs/tech-blog)

`kimi-k2.5`

## Breakthrough in Coding Capabilities

As a leading coding model in China, Kimi K2.5 builds upon its full-stack development and tooling ecosystem strengths, further enhancing frontend code quality and design expressiveness. This major breakthrough enables the generation of fully functional, visually appealing interactive user interfaces directly from natural language, with precise control over complex effects such as dynamic layouts and scrolling animations.

## Ultra-Long Context Support

`kimi-k2.5`, `kimi-k2-0905-Preview`, `kimi-k2-turbo-preview`, `kimi-k2-thinking`, and `kimi-k2-thinking-turbo` models all provide a 256K context window.

## Long-Thinking Capabilities

kimi-k2.5 still has strong reasoning capabilities, supporting multi-step tool invocation and reasoning, excelling at solving complex problems, such as complex logical reasoning, mathematical problems, and code writing.

## Example Usage

Here is a complete usage example to help you quickly get started with the Kimi K2.5 model.

### Install the OpenAI SDK

Kimi API is fully compatible with OpenAI's API format. You can install the OpenAI SDK as follows:

```bash
pip install --upgrade 'openai>=1.0'
```

### Verify the Installation

```bash
python -c 'import openai; print("version =",openai.__version__)'
```

# The output may be version = 1.10.0, indicating the OpenAI SDK was installed successfully and your Python environment is using OpenAI SDK v1.10.0.

### Image Understanding Code Example

```python
import os
import base64

from openai import OpenAI

client = OpenAI(
    api_key=os.environ.get("MOONSHOT_API_KEY"),
    base_url="https://api.moonshot.ai/v1",
)

# Replace kimi.png with the path to the image you want Kimi to analyze
image_path = "kimi.png"

with open(image_path, "rb") as f:
    image_data = f.read()

# Use the standard library base64.b64encode function to encode the image into base64 format
image_url = f"data:image/{os.path.splitext(image_path)[1]};base64,{base64.b64encode(image_data).decode('utf-8')}"


completion = client.chat.completions.create(
    model="kimi-k2.5",
    messages=[
        {"role": "system", "content": "You are Kimi."},
        {
            "role": "user",
            # Note: content is changed from str type to a list containing multiple content parts.
            # Image (image_url) is one part, and text is another part.
            "content": [
                {
                    "type": "image_url",  # <-- Use image_url type to upload images, with content as base64-encoded image data
                    "image_url": {
                        "url": image_url,
                    },
                },
                {
                    "type": "text",
                    "text": "Please describe the content of the image.",  # <-- Use text type to provide text instructions
                },
            ],
        },
    ],
)

print(completion.choices[0].message.content)
```

If your code runs successfully with no errors, you will see output similar to the following:

`[Image description output]`

### Video Understanding Code Example

```python
import os
import base64

from openai import OpenAI

client = OpenAI(
    api_key=os.environ.get("MOONSHOT_API_KEY"),
    base_url="https://api.moonshot.ai/v1",
)

# Replace kimi.mp4 with the path to the video you want Kimi to analyze
video_path = "kimi.mp4"

with open(video_path, "rb") as f:
    video_data = f.read()

# Use the standard library base64.b64encode function to encode the video into base64 format
video_url = f"data:video/{os.path.splitext(video_path)[1]};base64,{base64.b64encode(video_data).decode('utf-8')}"


completion = client.chat.completions.create(
    model="kimi-k2.5",
    messages=[
        {"role": "system", "content": "You are Kimi."},
        {
            "role": "user",
            # Note: content is changed from str type to a list containing multiple content parts.
            # Video (video_url) is one part, and text is another part.
            "content": [
                {
                    "type": "video_url",  # <-- Use video_url type to upload videos, with content as base64-encoded video data
                    "video_url": {
                        "url": video_url,
                    },
                },
                {
                    "type": "text",
                    "text": "Please describe the content of the video.",  # <-- Use text type to provide text instructions
                },
            ],
        },
    ],
)

print(completion.choices[0].message.content)
```

## Best Practices

### Supported Formats

- **Images**: `png`, `jpeg`, `webp`, `gif`.
- **Videos**: `mp4`, `mpeg`, `mov`, `avi`, `x-flv`, `mpg`, `webm`, `wmv`, `3gpp`.

### Token Calculation and Billing

Image and video token usage is dynamically calculated. You can use the token estimation API to check the expected token consumption for a request containing images or video before processing.

Generally, the higher the resolution of an image, the more tokens it will consume. For videos, the number of tokens depends on the number of keyframes and their resolution—the more keyframes and the higher their resolution, the greater the token consumption.

The Vision model uses the same billing method as the moonshot-v1 model series, with charges based on the total number of tokens processed. For more information, see:

For token pricing details, refer to [Model Pricing](#model-pricing).

### Recommended Resolution

We recommend that image resolution should not exceed 4k (4096×2160), and video resolution should not exceed 2k (2048×1080). Higher resolutions will only increase processing time and will not improve the model’s understanding.

### Upload File or Base64?

Due to the limitation on the overall size of the request body, for very large videos you must use the file upload method to utilize vision capabilities. For images or videos that will be referenced multiple times, it is recommended to use the file upload method. Regarding file upload limitations, please refer to the [File Upload documentation](https://api.moonshot.ai/docs/file-upload).

- **Image quantity limit**: The Vision model has no limit on the number of images, but ensure that the request body size does not exceed 100M.
- **URL-formatted images**: Not supported, currently only supports base64-encoded image content.

### Parameters Differences in Request Body

Parameters are listed in chat. However, behaviour of some parameters may be different in k2.5 models. We recommend using the default values instead of manually configuring these parameters.

Differences are listed below:

| Field               | Required | Description                                                                  | Type     | Values                                                                                                                       |
| :------------------ | :------- | :--------------------------------------------------------------------------- | :------- | :--------------------------------------------------------------------------------------------------------------------------- |
| `max_tokens`        | optional | The maximum number of tokens to generate for the chat completion.            | `int`    | Default to be 32k aka 32768                                                                                                  |
| `thinking`          | optional | **New!** This parameter controls if the thinking is enabled for this request | `object` | Default to be `{"type": "enabled"}`. Value can only be one of `{"type": "enabled"}` or `{"type": "disabled"}`                |
| `temperature`       | optional | The sampling temperature to use                                              | `float`  | k2.5 model will use a fixed value 1.0, non-thinking mode will use a fixed value 0.6. Any other value will result in an error |
| `top_p`             | optional | A sampling method                                                            | `float`  | k2.5 model will use a fixed value 0.95. Any other value will result in an error                                              |
| `n`                 | optional | The number of results to generate for each input message                     | `int`    | k2.5 model will use a fixed value 1. Any other value will result in an error                                                 |
| `presence_penalty`  | optional | Penalizing new tokens based on whether they appear in the text               | `float`  | k2.5 model will use a fixed value 0.0. Any other value will result in an error                                               |
| `frequency_penalty` | optional | Penalizing new tokens based on their existing frequency in the text          | `float`  | k2.5 model will use a fixed value 0.0. Any other value will result in an error                                               |

### Tool Use Compatibility

When using tools, if the `thinking` parameter is set to `{"type": "enabled"}`, please note the following constraints to ensure model performance:

- `tool_choice` can only be set to `"auto"` or `"none"` (default is `"auto"`) to avoid conflicts between reasoning content and the specified `tool_choice`. Any other value will result in an error;
- During multi-step tool calling, you must keep the `reasoning_content` from the assistant message in the current turn's tool call within the context, otherwise an error will be thrown;
- The official builtin `$web_search` tool is temporarily incompatible with Kimi K2.5 thinking mode, you can choose to disable thinking mode first and then use the `$web_search` tool.

You can refer to [Use Thinking Models](https://api.moonshot.ai/docs/thinking) for correct usage of tool calling.

### Disable Thinking Capability Example

For the kimi-k2.5 model, you can disable thinking by specifying `"thinking": {"type": "disabled"}` in the request body:

```bash
curl https://api.moonshot.cn/v1/chat/completions \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $MOONSHOT_API_KEY" \
    -d '{
        "model": "kimi-k2.5",
        "messages": [
            {"role": "user", "content": "hello"}
        ],
        "thinking": {"type": "disabled"}
   }'
```

## Model Pricing

| Model       | Unit      | Input Price (Cache Hit) | Input Price (Cache Miss) | Output Price | Context Window |
| :---------- | :-------- | :---------------------- | :----------------------- | :----------- | :------------- |
| `kimi-k2.5` | 1M tokens | $0.10                   | $0.60                    | $3.00        | 262,144 tokens |
