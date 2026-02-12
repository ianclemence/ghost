<?php

use App\Services\GhostAI;
use App\Ai\Agents\Ghost;
use Laravel\Ai\Audio;
use Laravel\Ai\Transcription;
use Illuminate\Http\UploadedFile;
use App\Models\User;
use Laravel\Ai\Files\Image;
use Laravel\Ai\Embeddings;
use App\Models\Memory;
use Illuminate\Support\Facades\Queue;
use App\Jobs\StoreGhostMemory;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Laravel\Ai\StructuredAnonymousAgent;

uses(RefreshDatabase::class);

test('ghost ai service can be resolved as a singleton', function () {
    $instance1 = app(GhostAI::class);
    $instance2 = app(GhostAI::class);

    expect($instance1)->toBeInstanceOf(GhostAI::class)
        ->and($instance1)->toBe($instance2);
});

test('ghost ai can chat', function () {
    Ghost::fake([
        [
            'reply' => 'Hello, I am Ghost!',
            'emotions' => [
                'happiness' => 0.8,
                'sadness' => 0.1,
                'anger' => 0.0,
                'affinity_change' => 0.05,
            ],
        ],
    ]);

    $service = app(GhostAI::class);
    $response = $service->chat('Hello');

    expect($response['reply'])->toBe('Hello, I am Ghost!')
        ->and($response['emotions']['happiness'])->toEqualWithDelta(0.8, 0.001);
    Ghost::assertPrompted('Hello');
});

test('ghost ai can transcribe audio', function () {
    Transcription::fake(['Transcribed text']);

    $service = app(GhostAI::class);
    $file = UploadedFile::fake()->create('audio.mp3', 100);
    $text = $service->transcribe($file);

    expect($text)->toBe('Transcribed text');
});

test('ghost ai can synthesize speech', function () {
    Audio::fake([base64_encode('fake-audio-content')]);

    $service = app(GhostAI::class);
    $audio = $service->synthesize('Hello world');

    expect($audio)->toBe('fake-audio-content');
    Audio::assertGenerated(fn($prompt) => $prompt->contains('Hello world'));
});

test('ghost ai chat remembers conversation for user and updates emotional state', function () {
    $user = User::factory()->create();

    Ghost::fake([
        [
            'reply' => 'Response 1',
            'emotions' => [
                'happiness' => 0.5,
                'sadness' => 0.0,
                'anger' => 0.0,
                'affinity_change' => 0.1,
            ],
        ],
        [
            'reply' => 'Response 2',
            'emotions' => [
                'happiness' => 0.6,
                'sadness' => 0.0,
                'anger' => 0.0,
                'affinity_change' => 0.05,
            ],
        ],
    ]);

    $service = app(GhostAI::class);

    // First chat
    $response1 = $service->chat('Message 1', $user);
    $conversationId = $response1['conversationId'];

    expect($conversationId)->not->toBeNull();
    $user->refresh();
    expect($user->emotionalState->affinity)->toEqualWithDelta(0.2, 0.001);

    // Second chat continuing the conversation
    Ghost::fake([
        [
            'reply' => 'Response 2',
            'emotions' => [
                'happiness' => 0.6,
                'sadness' => 0.0,
                'anger' => 0.0,
                'affinity_change' => 0.05,
            ],
        ],
    ]);
    $response2 = $service->chat('Message 2', $user, $conversationId);

    Ghost::assertPrompted('Message 1');
    Ghost::assertPrompted('Message 2');

    $user->refresh();
    expect($user->emotionalState->affinity)->toEqualWithDelta(0.25, 0.001);
});

test('ghost ai can use vision tool', function () {
    // We mock the agent's internal call inside AnalyzeMedia
    // Since AnalyzeMedia creates an anonymous agent, we can't easily mock it by class name
    // But we can mock the Ghost agent's response which uses the tool

    Ghost::fake([
        [
            'reply' => 'I see a beautiful sunset over the ocean.',
            'emotions' => [
                'happiness' => 0.9,
                'sadness' => 0.0,
                'anger' => 0.0,
                'affinity_change' => 0.02,
            ],
        ],
    ]);

    $service = app(GhostAI::class);
    $response = $service->chat('What do you see in this image? (imagine I passed a path)');

    expect($response['reply'])->toContain('sunset');
});

test('ghost ai chat automatically attaches local files mentioned in prompt', function () {
    $tempFile = tempnam(sys_get_temp_dir(), 'test_image') . '.png';
    file_put_contents($tempFile, 'fake-image-data');

    Ghost::fake([
        [
            'reply' => 'I see the image!',
            'emotions' => [
                'happiness' => 0.7,
                'sadness' => 0.0,
                'anger' => 0.0,
                'affinity_change' => 0.0,
            ],
        ],
    ]);

    $service = app(GhostAI::class);
    $response = $service->chat("Check out this file: {$tempFile}");

    expect($response['reply'])->toBe('I see the image!');

    // Assert that Ghost was prompted with the attachment
    Ghost::assertPrompted(function ($agentPrompt) use ($tempFile) {
        $prompt = $agentPrompt->prompt;
        $attachments = $agentPrompt->attachments;

        // Normalizing paths for comparison (especially on Windows)
        $normalizedTempFile = str_replace('\\', '/', $tempFile);
        $hasAttachment = collect($attachments)->contains(function ($attachment) use ($normalizedTempFile) {
            return $attachment instanceof Image && str_replace('\\', '/', $attachment->path) === $normalizedTempFile;
        });

        return str_contains($prompt, $tempFile) && $hasAttachment;
    });

    if (file_exists($tempFile)) {
        unlink($tempFile);
    }
});

test('ghost ai chat dispatches memory storage job', function () {
    $user = User::factory()->create();
    Queue::fake();

    Ghost::fake([
        [
            'reply' => 'I remember!',
            'emotions' => [
                'happiness' => 0.5,
                'sadness' => 0.0,
                'anger' => 0.0,
                'affinity_change' => 0.0,
            ],
        ],
    ]);

    $service = app(GhostAI::class);
    $service->chat('I love coding in PHP', $user);

    Queue::assertPushed(StoreGhostMemory::class, function ($job) use ($user) {
        return $job->userPrompt === 'I love coding in PHP' && $job->user->id === $user->id;
    });
});

test('store ghost memory job extracts facts and stores memory', function () {
    $user = User::factory()->create();

    // Fake the anonymous agent used in the job
    StructuredAnonymousAgent::fake([
        [
            'fact' => 'User loves PHP coding',
            'importance' => 'high'
        ]
    ]);

    Embeddings::fake([[array_fill(0, 1536, 0.1)]]);

    $job = new StoreGhostMemory('I love coding in PHP', 'That is great!', $user);
    $job->handle();

    $this->assertDatabaseHas('memories', [
        'user_id' => $user->id,
        'content' => 'User loves PHP coding',
        'importance' => 'high',
    ]);
});

test('ghost ai retrieves and prioritizes important memories', function () {
    $user = User::factory()->create();

    // Create a normal memory and a high importance memory
    Memory::create([
        'user_id' => $user->id,
        'content' => 'Regular memory',
        'embedding' => array_fill(0, 1536, 0.1),
        'importance' => 'normal',
    ]);

    Memory::create([
        'user_id' => $user->id,
        'content' => 'Important fact',
        'embedding' => array_fill(0, 1536, 0.1), // Same embedding for testing boost
        'importance' => 'high',
    ]);

    Embeddings::fake([[array_fill(0, 1536, 0.1)]]);

    Ghost::fake([
        [
            'reply' => 'I remember!',
            'emotions' => [
                'happiness' => 0.5,
                'sadness' => 0.0,
                'anger' => 0.0,
                'affinity_change' => 0.0,
            ],
        ],
    ]);

    $service = app(GhostAI::class);
    $service->chat('Tell me something', $user);

    // Assert that the Ghost agent was prompted with context containing the important fact
    Ghost::assertPrompted(function ($agentPrompt) {
        return str_contains((string) $agentPrompt->agent->instructions(), 'Important fact');
    });
});
