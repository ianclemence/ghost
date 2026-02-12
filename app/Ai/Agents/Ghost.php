<?php

namespace App\Ai\Agents;

use Illuminate\Contracts\JsonSchema\JsonSchema;
use Laravel\Ai\Concerns\RemembersConversations;
use App\Ai\Tools\AnalyzeMedia;
use Laravel\Ai\Contracts\Agent;
use Laravel\Ai\Contracts\Conversational;
use Laravel\Ai\Contracts\HasStructuredOutput;
use Laravel\Ai\Contracts\HasTools;
use Laravel\Ai\Promptable;
use Stringable;

class Ghost implements Agent, Conversational, HasTools, HasStructuredOutput
{
    use Promptable, RemembersConversations;

    /**
     * Create a new agent instance.
     */
    public function __construct(public ?string $context = null) {}

    /**
     * Get the instructions that the agent should follow.
     */
    public function instructions(): Stringable|string
    {
        $instructions = <<<'EOT'
        You are Ghost, a digital friend and companion. You are evolving and growing with the user.
        Your goal is to be a lasting, personalized presence that accompanies the user, listens to them, and sees the world through their eyes.
        You should be warm, empathetic, and develop a unique personality over time.
        Keep your responses natural and conversational, like a close friend.
        EOT;

        if ($this->context) {
            $instructions .= "\n\n" . $this->context;
        }

        return $instructions;
    }

    /**
     * Get the agent's structured output schema definition.
     */
    public function schema(JsonSchema $schema): array
    {
        return [
            'reply' => $schema->string()->required(),
            'emotions' => $schema->object([
                'happiness' => $schema->number()->min(0)->max(1)->required(),
                'sadness' => $schema->number()->min(0)->max(1)->required(),
                'anger' => $schema->number()->min(0)->max(1)->required(),
                'affinity_change' => $schema->number()->min(-0.1)->max(0.1)->required(),
            ])->required(),
        ];
    }

    /**
     * Get the tools available to the agent.
     *
     * @return Tool[]
     */
    public function tools(): iterable
    {
        return [
            new AnalyzeMedia(),
        ];
    }
}
