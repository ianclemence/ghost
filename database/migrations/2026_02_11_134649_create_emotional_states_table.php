<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     */
    public function up(): void
    {
        Schema::create('emotional_states', function (Blueprint $table) {
            $table->id();
            $table->foreignId('user_id')->constrained()->cascadeOnDelete();
            $table->decimal('happiness', 4, 3)->default(0.5);
            $table->decimal('sadness', 4, 3)->default(0.0);
            $table->decimal('anger', 4, 3)->default(0.0);
            $table->decimal('affinity', 4, 3)->default(0.1);
            $table->timestamp('last_interaction_at')->nullable();
            $table->timestamps();
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('emotional_states');
    }
};
