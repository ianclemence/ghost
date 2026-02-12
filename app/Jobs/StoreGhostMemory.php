<?php

namespace App\Jobs;

use App\Models\Memory;
use App\Models\User;
use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Foundation\Queue\Queueable;
use Illuminate\Support\Str;
use function Laravel\Ai\agent;

class StoreGhostMemory implements ShouldQueue
{
    use Queueable;

    /**
     * Create a new job instance.
     */
    public function __construct(
        public string $userPrompt,
        public string $ghostReply,
        public User $user
    ) {}

    /**
     * Execute the job.
     */
    public function handle(): void
    {
        try {
            // Extract facts or significant information from the interaction
            $analysis = agent(
                instructions: 'Extract any significant facts, preferences, or personal details about the user from this interaction. If no new information is present, return "none". Also determine importance (low, normal, high).',
                schema: fn($schema) => [
                    'fact' => $schema->string()->required(),
                    'importance' => $schema->string()->required(),
                ]
            )->prompt("User: {$this->userPrompt}\nGhost: {$this->ghostReply}");

            $content = $analysis['fact'] !== 'none'
                ? $analysis['fact']
                : "User: {$this->userPrompt}\nGhost: {$this->ghostReply}";

            $importance = $analysis['importance'] ?? 'normal';

            $embedding = Str::of($content)->toEmbeddings();

            Memory::create([
                'user_id' => $this->user->id,
                'content' => $content,
                'embedding' => $embedding,
                'importance' => $importance,
            ]);
        } catch (\Exception $e) {
            logger()->error('Failed to store memory in job: ' . $e->getMessage());
        }
    }
}
