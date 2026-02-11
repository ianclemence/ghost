<?php

use App\Services\GhostAI;
use App\Ai\Agents\Ghost;
use Laravel\Ai\Audio;
use Laravel\Ai\Transcription;
use Illuminate\Http\UploadedFile;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;

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
