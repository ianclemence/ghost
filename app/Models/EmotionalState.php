<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

class EmotionalState extends Model
{
    /**
     * The attributes that are mass assignable.
     *
     * @var array<int, string>
     */
    protected $fillable = [
        'user_id',
        'happiness',
        'sadness',
        'anger',
        'affinity',
        'last_interaction_at',
    ];

    /**
     * The attributes that should be cast.
     *
     * @var array<string, string>
     */
    protected $casts = [
        'happiness' => 'float',
        'sadness' => 'float',
        'anger' => 'float',
        'affinity' => 'float',
        'last_interaction_at' => 'datetime',
    ];

    /**
     * Get the user that owns the emotional state.
     */
    public function user(): BelongsTo
    {
        return $this->belongsTo(User::class);
    }
}
