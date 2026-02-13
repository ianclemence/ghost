<?php

namespace App\Services;

use App\Ai\Agents\Ghost;
use App\Models\EmotionalState;
use Laravel\Ai\Audio;
use Laravel\Ai\Transcription;
use App\Jobs\StoreGhostMemory;
use App\Models\Memory;
use Illuminate\Support\Str;
use Laravel\Ai\Embeddings;
use Laravel\Ai\Files\Image;
use Laravel\Ai\Files\Document;
use Illuminate\Http\UploadedFile;
use Laravel\Ai\Responses\AgentResponse;

use function Laravel\Ai\agent;

class GhostAI
{
    /**
     * The singleton instance of the service.
     */
    protected static ?GhostAI $instance = null;

    /**
     * Get the singleton instance.
     */
    public static function getInstance(): GhostAI
    {
        if (static::$instance === null) {
            static::$instance = new static();
        }

        return static::$instance;
    }

    /**
     * Prompt the Ghost agent and update emotional state.
     */
    public function chat(string $prompt, $user = null, ?string $conversationId = null): array
    {
        $context = null;

        // 1. Retrieve relevant memories (RAG)
        if ($user) {
            $memories = $this->retrieveMemories($prompt, $user);
            if ($memories->isNotEmpty()) {
                $context = "Relevant memories of past interactions:\n" . $memories->pluck('content')->implode("\n---\n");
            }
        }

        $agent = Ghost::make(context: $context);

        if ($user && $conversationId) {
            $agent->continue($conversationId, as: $user);
        } elseif ($user) {
            $agent->forUser($user);
        }

        $attachments = $this->detectAttachments($prompt);

        $response = $agent->prompt(
            prompt: $prompt,
            attachments: $attachments,
            model: !empty($attachments) ? 'kimi-k2.5' : null
        );
        $data = $response->structured ?? [];

        if ($user && !empty($data)) {
            // 2. Update emotional state
            $this->updateEmotionalState($user, $data['emotions'] ?? []);

            // 3. Queue this interaction for memory storage
            StoreGhostMemory::dispatch($prompt, $data['reply'] ?? (string) $response, $user);
        }

        return [
            'reply' => $data['reply'] ?? (string) $response,
            'emotions' => $data['emotions'] ?? [],
            'conversationId' => $response->conversationId ?? $conversationId,
        ];
    }

    /**
     * Retrieve relevant memories using cosine similarity.
     */
    protected function retrieveMemories(string $prompt, $user, int $limit = 5)
    {
        try {
            $queryEmbedding = Str::of($prompt)->toEmbeddings();

            // Retrieve a larger pool to filter through
            $memories = Memory::where('user_id', $user->id)
                ->whereNotNull('embedding')
                ->latest()
                ->limit(100)
                ->get();

            return $memories->map(function ($memory) use ($queryEmbedding) {
                $similarity = $this->cosineSimilarity($queryEmbedding, $memory->embedding);

                // Boost similarity for important memories
                if ($memory->importance === 'high') {
                    $similarity += 0.1;
                } elseif ($memory->importance === 'low') {
                    $similarity -= 0.05;
                }

                $memory->similarity = $similarity;
                return $memory;
            })
                ->filter(fn($memory) => $memory->similarity > 0.65)
                ->sortByDesc('similarity')
                ->take($limit);
        } catch (\Exception $e) {
            logger()->error('Failed to retrieve memories: ' . $e->getMessage());
            return collect();
        }
    }

    /**
     * Calculate cosine similarity between two vectors.
     */
    protected function cosineSimilarity(array $vec1, array $vec2): float
    {
        $dotProduct = 0;
        $norm1 = 0;
        $norm2 = 0;

        foreach ($vec1 as $i => $val) {
            $dotProduct += $val * ($vec2[$i] ?? 0);
            $norm1 += $val ** 2;
            $norm2 += ($vec2[$i] ?? 0) ** 2;
        }

        if ($norm1 == 0 || $norm2 == 0) {
            return 0;
        }

        return $dotProduct / (sqrt($norm1) * sqrt($norm2));
    }

    /**
     * Detect file paths in the prompt and return them as attachments.
     */
    protected function detectAttachments(string $prompt): array
    {
        $attachments = [];
        $imageExtensions = ['png', 'jpg', 'jpeg', 'webp', 'gif'];
        $videoExtensions = ['mp4', 'mpeg', 'mov', 'avi', 'mpg', 'webm', 'wmv'];
        $allExtensions = array_merge($imageExtensions, $videoExtensions);

        // Match common path patterns: /path/to/file.ext, C:\path\to\file.ext, or relative paths with extension
        $regex = '/(?:[a-zA-Z]:)?[\\\\\/][^ \t\n\r\f\v"\'<>|]+\.(?:' . implode('|', $allExtensions) . ')/i';

        if (preg_match_all($regex, $prompt, $matches)) {
            foreach ($matches[0] as $path) {
                // Remove potential surrounding quotes or punctuation
                $cleanPath = trim($path, " \t\n\r\f\v\"'.,!?;:");

                if (file_exists($cleanPath)) {
                    $extension = strtolower(pathinfo($cleanPath, PATHINFO_EXTENSION));

                    if (in_array($extension, $imageExtensions)) {
                        $attachments[] = Image::fromPath($cleanPath);
                    } elseif (in_array($extension, $videoExtensions)) {
                        $attachments[] = Document::fromPath($cleanPath);
                    }
                }
            }
        }

        return $attachments;
    }

    /**
     * Update the user's emotional state based on AI analysis.
     */
    protected function updateEmotionalState($user, array $emotions): void
    {
        $state = EmotionalState::firstOrCreate(
            ['user_id' => $user->id],
            [
                'happiness' => 0.5,
                'sadness' => 0.0,
                'anger' => 0.0,
                'affinity' => 0.1,
            ]
        );

        // Calculate new affinity before updating other fields to avoid using updated state
        $newAffinity = max(0, min(1, $state->affinity + ($emotions['affinity_change'] ?? 0)));

        $state->update([
            'happiness' => $emotions['happiness'] ?? $state->happiness,
            'sadness' => $emotions['sadness'] ?? $state->sadness,
            'anger' => $emotions['anger'] ?? $state->anger,
            'affinity' => $newAffinity,
            'last_interaction_at' => now(),
        ]);
    }

    /**
     * Transcribe audio to text.
     */
    public function transcribe(UploadedFile $audio): string
    {
        return (string) Transcription::fromUpload($audio)->generate();
    }

    /**
     * Synthesize text to audio.
     */
    public function synthesize(string $text, string $voice = 'female'): string
    {
        $audio = Audio::of($text);

        if ($voice === 'female') {
            $audio->female();
        } elseif ($voice === 'male') {
            $audio->male();
        } else {
            $audio->voice($voice);
        }

        return (string) $audio->generate();
    }
}
