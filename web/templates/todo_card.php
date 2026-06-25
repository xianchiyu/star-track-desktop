<div class="todo-card d-flex align-items-center" data-id="<?php echo htmlspecialchars($todo['id']); ?>" data-type="<?php echo $todo['task_type']; ?>">
    <div class="form-check">
        <input class="form-check-input" type="checkbox" <?php echo $todo['completed'] ? 'checked' : ''; ?> 
               data-id="<?php echo htmlspecialchars($todo['id']); ?>">
    </div>
    <div class="todo-content flex-grow-1">
        <?php echo htmlspecialchars($todo['title']); ?>
        <?php if ($todo['due_date']): ?>
            <small class="text-muted"><?php echo date('m-d', strtotime($todo['due_date'])); ?></small>
        <?php endif; ?>
    </div>
    <button type="button" class="btn btn-link text-danger delete-todo" data-id="<?php echo htmlspecialchars($todo['id']); ?>">
        <i class="bi bi-x"></i>
    </button>
</div>
