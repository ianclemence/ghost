<?php

namespace App\Ai\Tools;

use Illuminate\Contracts\JsonSchema\JsonSchema;
use function Laravel\Ai\agent;
use Laravel\Ai\Contracts\Tool;
use Laravel\Ai\Tools\Request;
use Laravel\Ai\Files\Image;
use Laravel\Ai\Files\Document;
use Stringable;

class AnalyzeMedia implements Tool
{
    /**
     * Get the description of the tool's purpose.
     */
    public function description(): Stringable|string
    {
        return 'Analyze and describe the content of an image or video file provided by the user.';
    }

    /**
     * Execute the tool.
     */
    public function handle(Request $request): Stringable|string
    {
        $path = $request->input('path');
        $prompt = $request->input('prompt', 'Describe the content of this media.');

        if (!file_exists($path)) {
            return "Error: File not found at path: {$path}";
        }

        $extension = strtolower(pathinfo($path, PATHINFO_EXTENSION));
        $isImage = in_array($extension, ['png', 'jpg', 'jpeg', 'webp', 'gif']);
        $isVideo = in_array($extension, ['mp4', 'mpeg', 'mov', 'avi', 'mpg', 'webm', 'wmv']);

        if ($isImage) {
            $attachment = Image::fromPath($path);
        } elseif ($isVideo) {
            // Video is handled via Document in some SDKs or specific media classes
            // For Kimi, both are often treated as multimodal attachments
            $attachment = Document::fromPath($path);
        } else {
            return "Error: Unsupported file type: {$extension}";
        }

        return (string) agent()
            ->prompt($prompt, attachments: [$attachment]);
    }

    /**
     * Get the tool's schema definition.
     */
    public function schema(JsonSchema $schema): array
    {
        return [
            'path' => $schema->string()->description('The absolute file path to the image or video to analyze.')->required(),
            'prompt' => $schema->string()->description('Specific instructions for the analysis (e.g., "What color is the car?").')->default('Describe the content of this media.'),
        ];
    }
}
