<?php

namespace App\Services;

use App\Ai\Agents\Ghost;
use App\Models\EmotionalState;
use Laravel\Ai\Audio;
use Laravel\Ai\Transcription;
use Illuminate\Http\UploadedFile;
use Laravel\Ai\Responses\AgentResponse;

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
        $agent = Ghost::make();

        if ($user && $conversationId) {
            $agent->continue($conversationId, as: $user);
        } elseif ($user) {
            $agent->forUser($user);
        }

        $response = $agent->prompt($prompt);
        $data = $response->structured ?? [];

        if ($user && !empty($data)) {
            $this->updateEmotionalState($user, $data['emotions'] ?? []);
        }

        return [
            'reply' => $data['reply'] ?? (string) $response,
            'emotions' => $data['emotions'] ?? [],
            'conversationId' => $response->conversationId ?? $conversationId,
        ];
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
